package desktop

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"time"

	"yanecompanion/internal/lifecycle"
)

type activation struct{ Address, Token string }

// Instance outlives the playback runtime, including startup failures. This
// local-only endpoint can show the GUI; it has no consent or playback actions.
type Instance struct {
	Show      chan struct{}
	server    *http.Server
	lock      *os.File
	path      string
	closeOnce sync.Once
}

func OpenInstance(state string) (_ *Instance, result error) {
	lock, err := lifecycle.Lock(filepath.Join(state, "desktop.lock"))
	if err != nil {
		return nil, err
	}
	defer func() {
		if result != nil {
			lock.Close()
		}
	}()
	listener, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		return nil, err
	}
	defer func() {
		if result != nil {
			listener.Close()
		}
	}()
	var data [32]byte
	if _, err := rand.Read(data[:]); err != nil {
		return nil, err
	}
	proof := activation{Address: listener.Addr().String(), Token: hex.EncodeToString(data[:])}
	i := &Instance{Show: make(chan struct{}, 1), lock: lock, path: filepath.Join(state, "desktop-control.json")}
	i.server = &http.Server{ReadHeaderTimeout: time.Second, ReadTimeout: time.Second, WriteTimeout: time.Second, IdleTimeout: time.Second, MaxHeaderBytes: 8 << 10,
		Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Cache-Control", "no-store")
			if r.Host != proof.Address || len(r.Header.Values("Origin")) != 0 || len(r.Header.Values("Authorization")) != 1 || subtle.ConstantTimeCompare([]byte(r.Header.Get("Authorization")), []byte("Bearer "+proof.Token)) != 1 {
				http.Error(w, "LOCAL_AUTH_REQUIRED", 403)
				return
			}
			if r.Method != "POST" || r.URL.Path != "/show" || r.URL.RawQuery != "" || r.URL.ForceQuery || r.URL.RawPath != "" || r.ContentLength != 0 {
				http.Error(w, "NOT_FOUND", 404)
				return
			}
			select {
			case i.Show <- struct{}{}:
			default:
			}
			w.WriteHeader(202)
		})}
	if err := writePrivateJSON(i.path, proof); err != nil {
		return nil, err
	}
	go i.server.Serve(listener)
	return i, nil
}

func (i *Instance) Close() {
	i.closeOnce.Do(func() {
		_ = i.server.Close()
		_ = os.Remove(i.path)
		_ = i.lock.Close()
	})
}

func Activate(ctx context.Context, state string) error {
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	client := &http.Client{Timeout: time.Second, Transport: &http.Transport{Proxy: nil}, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	defer client.CloseIdleConnections()
	for {
		path := filepath.Join(state, "desktop-control.json")
		var data []byte
		info, err := os.Lstat(path)
		if err == nil && info.Mode().IsRegular() && info.Size() <= 1024 {
			data, err = os.ReadFile(path)
		}
		var proof activation
		if err == nil && len(data) <= 1024 && json.Unmarshal(data, &proof) == nil && validActivation(proof) {
			r, err := http.NewRequestWithContext(ctx, "POST", "http://"+proof.Address+"/show", nil)
			if err == nil {
				r.Header.Set("Authorization", "Bearer "+proof.Token)
				response, err := client.Do(r)
				if err == nil {
					response.Body.Close()
					if response.StatusCode == 202 {
						return nil
					}
				}
			}
		}
		select {
		case <-ctx.Done():
			return errors.New("DESKTOP_UNAVAILABLE")
		case <-time.After(50 * time.Millisecond):
		}
	}
}

func validActivation(proof activation) bool {
	host, port, err := net.SplitHostPort(proof.Address)
	number, portErr := strconv.Atoi(port)
	token, tokenErr := hex.DecodeString(proof.Token)
	return err == nil && host == "127.0.0.1" && portErr == nil && number > 0 && number <= 65535 && strconv.Itoa(number) == port && tokenErr == nil && len(token) == 32 && hex.EncodeToString(token) == proof.Token
}

func writePrivateJSON(path string, value any) error {
	data, err := json.Marshal(value)
	if err != nil {
		return err
	}
	file, err := os.CreateTemp(filepath.Dir(path), ".desktop-")
	if err != nil {
		return err
	}
	defer os.Remove(file.Name())
	if err = file.Chmod(0600); err == nil {
		_, err = file.Write(data)
	}
	if err == nil {
		err = file.Sync()
	}
	closeErr := file.Close()
	if err != nil {
		return err
	}
	if closeErr != nil {
		return closeErr
	}
	return replaceSettings(file.Name(), path)
}
