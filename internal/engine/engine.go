package engine

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
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"yanecompanion/internal/lifecycle"
	"yanecompanion/internal/locator"
	"yanecompanion/release"
)

type Engine struct {
	process           *lifecycle.Process
	client            *http.Client
	address, password string
}

// Only validated input reaches this private adapter; HTTP ownership lives in sessions.
func (e *Engine) Admit(ctx context.Context, input locator.Input) error {
	if !input.Valid() {
		return locator.ErrUnsafe
	}
	// Settle this private mutation before a cancellation can drop/reuse its hash.
	mutation, cancel := context.WithTimeout(context.WithoutCancel(ctx), 15*time.Second)
	defer cancel()
	if err := e.admit(mutation, input); err != nil {
		e.process.Stop() // A lost engine response must never leave an unowned torrent.
		return err
	}
	return nil
}

func Start(ctx context.Context, executable, directory string) (*Engine, error) {
	var secret [32]byte
	if _, err := rand.Read(secret[:]); err != nil {
		return nil, err
	}
	password := hex.EncodeToString(secret[:])
	if err := os.MkdirAll(directory, 0700); err != nil {
		return nil, err
	}
	accounts, _ := json.Marshal(map[string]string{"companion": password})
	if err := os.WriteFile(filepath.Join(directory, "accs.db"), accounts, 0600); err != nil {
		return nil, err
	}
	settings := map[string]any{"BitTorr": map[string]any{
		"CacheSize": 256 << 20, "UseDisk": false, "RemoveCacheOnDrop": true,
		"EnableDLNA": false, "EnableBonjour": false, "EnableLPD": false, "EnableIPv6": false,
		"DisableUPNP": true, "DisableDHT": true, "DisablePEX": true, "DisableUpload": true,
		"DisableUTP": true, "EnableRutorSearch": false, "EnableTorznabSearch": false,
		"RetrackersMode": 0, "TrackersListURL": "disabled", "DefaultTrackers": "",
		"StoreSettingsInJson": true, "StoreViewedInJson": false, "TrackTimecode": false,
		"ConnectionsLimit": 25, "PeersListenPort": 0, "TorrentDisconnectTimeout": 30,
	}}
	data, _ := json.Marshal(settings)
	if err := os.WriteFile(filepath.Join(directory, "settings.json"), data, 0600); err != nil {
		return nil, err
	}
	listener, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		return nil, err
	}
	port := listener.Addr().(*net.TCPAddr).Port
	listener.Close()
	cmd := exec.Command(executable, "--ip", "127.0.0.1", "--port", strconv.Itoa(port), "--path", directory, "--httpauth", "--torrentaddr", "127.0.0.1:0")
	cmd.Dir = directory
	process, err := lifecycle.Start(cmd)
	if err != nil {
		return nil, errors.New("ENGINE_UNAVAILABLE")
	}
	transport := &http.Transport{Proxy: nil, DialContext: (&net.Dialer{Timeout: time.Second}).DialContext, DisableKeepAlives: true}
	e := &Engine{process, &http.Client{Transport: transport, Timeout: time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}, fmt.Sprintf("http://127.0.0.1:%d", port), password}
	ctx, cancel := context.WithTimeout(ctx, 12*time.Second)
	defer cancel()
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()
	for {
		if e.ready(ctx) {
			return e, nil
		}
		select {
		case <-ctx.Done():
			e.Close()
			return nil, errors.New("ENGINE_UNAVAILABLE")
		case <-process.Done():
			e.Close()
			return nil, errors.New("ENGINE_UNAVAILABLE")
		case <-ticker.C:
		}
	}
}

func (e *Engine) ready(ctx context.Context) bool {
	request, _ := http.NewRequestWithContext(ctx, "GET", e.address+"/echo", nil)
	request.SetBasicAuth("companion", e.password)
	response, err := e.client.Do(request)
	if err != nil {
		return false
	}
	defer response.Body.Close()
	body, err := io.ReadAll(io.LimitReader(response.Body, 256))
	if err != nil || response.StatusCode != 200 || strings.TrimSpace(string(body)) != release.EngineVersion {
		return false
	}
	// This startup check catches wrong credentials/builds. The bundled-engine
	// integration test separately verifies a known torrent with real metadata;
	// an unknown hash alone cannot prove protection of an active raw stream.
	for _, route := range []struct{ method, path, body string }{
		{"POST", "/torrents", `{"action":"list"}`},
		{"GET", "/stream?link=" + strings.Repeat("0", 40) + "&index=1&play", ""},
	} {
		request, _ = http.NewRequestWithContext(ctx, route.method, e.address+route.path, strings.NewReader(route.body))
		request.Header.Set("Content-Type", "application/json")
		response, err = e.client.Do(request)
		if err != nil {
			return false
		}
		response.Body.Close()
		if response.StatusCode != http.StatusUnauthorized {
			return false
		}
	}
	return true
}

func (e *Engine) Done() <-chan struct{} { return e.process.Done() }
func (e *Engine) Close()                { e.process.Stop(); e.client.CloseIdleConnections() }
