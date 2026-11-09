package app

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"yanecompanion/internal/api"
	"yanecompanion/internal/cache"
	"yanecompanion/internal/config"
	"yanecompanion/internal/engine"
	"yanecompanion/internal/lifecycle"
	"yanecompanion/internal/media"
	"yanecompanion/internal/pairing"
	localsessions "yanecompanion/internal/sessions"
	"yanecompanion/release"
)

func randomID(bytes int) (string, error) {
	data := make([]byte, bytes)
	if _, err := rand.Read(data); err != nil {
		return "", err
	}
	return hex.EncodeToString(data), nil
}

type Options struct {
	Paths             config.Paths
	BundleDir         string
	Origins           []string
	DevelopmentOrigin string
	Ready             func()
	OriginAllowed     func(string) bool
	Show              func()
	DesktopReady      func(*pairing.Store, *localsessions.Manager)
}

func Run(ctx context.Context, options Options) (result error) {
	if options.DevelopmentOrigin != "" && !api.ValidDevelopmentOrigin(options.DevelopmentOrigin) {
		return errors.New("INVALID_DEVELOPMENT_ORIGIN")
	}
	for _, origin := range options.Origins {
		if !api.ValidOrigin(origin) {
			return errors.New("INVALID_ORIGIN")
		}
	}
	if err := options.Paths.Create(); err != nil {
		return errors.New("STATE_UNAVAILABLE")
	}
	lock, err := lifecycle.Lock(filepath.Join(options.Paths.State, "instance.lock"))
	if err != nil {
		return err
	}
	defer lock.Close()
	pairings, err := pairing.Open(options.Paths.Config)
	if err != nil {
		return err
	}
	listener, err := net.Listen("tcp4", config.Address)
	if err != nil {
		return errors.New("PORT_IN_USE")
	}
	defer listener.Close()
	bundle, err := release.Verify(options.BundleDir)
	if err != nil {
		return err
	}
	sessions, err := cache.Open(filepath.Join(options.Paths.Cache, "sessions"))
	if err != nil {
		return err
	}
	defer sessions.Close()
	if err := sessions.Cleanup(); err != nil {
		return errors.New("CACHE_CLEANUP_FAILED")
	}
	if err := sessions.Check(); err != nil {
		return err
	}
	boot, err := randomID(16)
	if err != nil {
		return err
	}
	runDir, err := sessions.Create("run-" + boot)
	if err != nil {
		return err
	}
	defer func() {
		if err := sessions.Cleanup(); err != nil && result == nil {
			result = errors.New("CACHE_CLEANUP_FAILED")
		}
	}()
	localToken, err := randomID(32)
	if err != nil {
		return err
	}
	worker, err := engine.Start(ctx, bundle.Engine, filepath.Join(runDir, "engine"))
	if err != nil {
		return err
	}
	defer worker.Close()
	admissions := localsessions.NewWithMedia(worker, &media.Service{Runner: media.New(bundle.FFmpeg, bundle.FFprobe), Backend: worker, Directory: runDir})
	defer admissions.Close()
	pairings.OnRevoke = admissions.Revoke
	controlPath := filepath.Join(options.Paths.State, "control.json")
	control, _ := json.Marshal(map[string]string{"token": localToken, "bootId": boot})
	temporary, err := os.CreateTemp(options.Paths.State, ".control-")
	if err != nil {
		return errors.New("STATE_UNAVAILABLE")
	}
	tmpName := temporary.Name()
	defer os.Remove(tmpName)
	if err := temporary.Chmod(0600); err != nil {
		temporary.Close()
		return err
	}
	if _, err := temporary.Write(control); err != nil {
		temporary.Close()
		return err
	}
	if err := temporary.Close(); err != nil {
		return err
	}
	// The lock excludes all other host writers. Windows Rename cannot replace
	// an old file, so explicitly remove only our prior boot credential file.
	if err := os.Remove(controlPath); err != nil && !os.IsNotExist(err) {
		return errors.New("STATE_UNAVAILABLE")
	}
	if err := os.Rename(tmpName, controlPath); err != nil {
		return errors.New("STATE_UNAVAILABLE")
	}
	defer os.Remove(controlPath)
	stop := make(chan struct{}, 1)
	server := &http.Server{Handler: api.Handler{BootID: boot, LocalToken: localToken, Origins: options.Origins, DevelopmentOrigin: options.DevelopmentOrigin, Stop: stop, Pairings: pairings, Management: &api.Management{}, Sessions: admissions, OriginAllowed: options.OriginAllowed, Show: options.Show}, ReadHeaderTimeout: 2 * time.Second, ReadTimeout: 3 * time.Second, WriteTimeout: 3 * time.Second, IdleTimeout: 5 * time.Second, MaxHeaderBytes: 8 << 10}
	serveErr := make(chan error, 1)
	go func() { serveErr <- server.Serve(listener) }()
	defer server.Close()
	if options.DesktopReady != nil {
		options.DesktopReady(pairings, admissions)
	}
	if options.Ready != nil {
		options.Ready()
	}
	monitor := time.NewTicker(time.Second)
	defer monitor.Stop()
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-stop:
			shutdown, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			defer cancel()
			return server.Shutdown(shutdown)
		case <-worker.Done():
			return errors.New("ENGINE_UNAVAILABLE")
		case err := <-admissions.Errors():
			return err
		case err := <-serveErr:
			if errors.Is(err, http.ErrServerClosed) {
				return nil
			}
			return errors.New("HTTP_UNAVAILABLE")
		case <-monitor.C:
			admissions.Tick()
			if err := sessions.Check(); err != nil {
				return err
			}
		}
	}
}

func Control(ctx context.Context, paths config.Paths, action string, output io.Writer) error {
	if action != "status" && action != "exit" && action != "manage" && action != "show" {
		return errors.New("INVALID_COMMAND")
	}
	data, err := os.ReadFile(filepath.Join(paths.State, "control.json"))
	if err != nil || len(data) > 1024 {
		return errors.New("NOT_RUNNING")
	}
	var credential struct {
		Token  string `json:"token"`
		BootID string `json:"bootId"`
	}
	if json.Unmarshal(data, &credential) != nil || len(credential.Token) != 64 || len(credential.BootID) != 32 {
		return errors.New("STATE_UNAVAILABLE")
	}
	method := "GET"
	if action == "exit" || action == "manage" || action == "show" {
		method = "POST"
	}
	route := "/local/" + action
	if action == "manage" {
		route = "/local/manage/open"
	}
	request, _ := http.NewRequestWithContext(ctx, method, "http://"+config.Address+route, nil)
	request.Header.Set("Authorization", "Bearer "+credential.Token)
	client := &http.Client{Timeout: 3 * time.Second, Transport: &http.Transport{Proxy: nil}, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	defer client.CloseIdleConnections()
	response, err := client.Do(request)
	if err != nil {
		return errors.New("NOT_RUNNING")
	}
	defer response.Body.Close()
	if response.StatusCode != 200 && response.StatusCode != 202 {
		return errors.New("CONTROL_FAILED")
	}
	if action == "manage" {
		var ticket struct {
			Token string `json:"managementToken"`
		}
		if json.NewDecoder(io.LimitReader(response.Body, 1024)).Decode(&ticket) != nil || len(ticket.Token) != 64 {
			return errors.New("CONTROL_FAILED")
		}
		if err := openManagement("http://" + config.Address + "/local/manage#" + ticket.Token); err != nil {
			return errors.New("MANAGEMENT_BROWSER_UNAVAILABLE")
		}
	}
	if action == "status" {
		var status struct {
			Product string `json:"product"`
			BootID  string `json:"bootId"`
		}
		if json.NewDecoder(io.LimitReader(response.Body, 1024)).Decode(&status) != nil || status.Product != "yaneCompanion" || status.BootID != credential.BootID {
			return errors.New("CONTROL_FAILED")
		}
	}
	_, err = fmt.Fprintln(output, "yaneCompanion", action, "OK")
	return err
}
