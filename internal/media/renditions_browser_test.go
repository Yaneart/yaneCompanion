package media

import (
	"context"
	"encoding/json"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// Private opt-in bridge to the real production producers. It deliberately has
// no torrent engine, normal profile, controller token or application credentials.
// Origin/read-grant protections are exercised separately by the API contract test.
func serveRenditionBrowser(t *testing.T, ctx context.Context, r *Renditions, directory string) {
	t.Helper()
	listener, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	server := &http.Server{ReadHeaderTimeout: 2 * time.Second, Handler: http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type")
		w.Header().Set("Cache-Control", "no-store")
		if request.Method == "OPTIONS" {
			w.WriteHeader(204)
			return
		}
		if request.URL.Path == "/select" && request.Method == "POST" {
			var target struct {
				AudioRef string  `json:"audioRef"`
				Position float64 `json:"positionSeconds"`
			}
			if json.NewDecoder(http.MaxBytesReader(w, request.Body, 1024)).Decode(&target) != nil {
				w.WriteHeader(400)
				return
			}
			value, err := r.SelectAudio(request.Context(), target.AudioRef, target.Position)
			if err != nil {
				http.Error(w, err.Error(), 422)
				return
			}
			_ = json.NewEncoder(w).Encode(value)
			return
		}
		r.Serve(w, request, strings.TrimPrefix(request.URL.Path, "/"))
	})}
	go func() { _ = server.Serve(listener) }()
	defer server.Close()
	if err = os.MkdirAll(directory, 0700); err != nil {
		t.Fatal(err)
	}
	data, _ := json.Marshal(map[string]any{"url": "http://" + listener.Addr().String() + "/", "audioTracks": r.probe.Audio, "timeline": r.Status().Timeline})
	if err = os.WriteFile(filepath.Join(directory, "stand.json"), data, 0600); err != nil {
		t.Fatal(err)
	}
	t.Log("private rendition browser stand ready")
	for {
		if _, err := os.Stat(filepath.Join(directory, "stop")); err == nil {
			return
		}
		select {
		case <-ctx.Done():
			t.Fatal("browser deadline")
		case <-time.After(100 * time.Millisecond):
		}
	}
}
