package api

import (
	"encoding/json"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"yanecompanion/internal/config"
	"yanecompanion/internal/locator"
	"yanecompanion/internal/sessions"
)

// Private opt-in browser fixture, like the existing audio review. No normal
// profile/port is used; production Handler still verifies its canonical Host.
func serveResilienceReview(t *testing.T, m *sessions.Manager, directory string) {
	t.Helper()
	if !filepath.IsAbs(directory) {
		t.Fatal("review directory must be absolute")
	}
	listener, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	site := "http://" + listener.Addr().String()
	c, err := m.CreateController(site, "isolated-browser")
	if err != nil {
		listener.Close()
		t.Fatal(err)
	}
	a, err := m.Create(site, c.Token, "123e4567-e89b-42d3-a456-426614174007", []byte("browser"), locator.Locator{Kind: "magnet", Value: "magnet:?xt=urn:btih:" + strings.Repeat("a", 40)})
	if err != nil {
		listener.Close()
		t.Fatal(err)
	}
	h := pairingHandler(t)
	h.DevelopmentOrigin, h.Sessions = site, m
	files := http.StripPrefix("/review/", http.FileServer(http.Dir(directory)))
	server := &http.Server{ReadHeaderTimeout: 2 * time.Second, Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/review/") {
			files.ServeHTTP(w, r)
			return
		}
		r.Host = config.Address
		h.ServeHTTP(w, r)
	})}
	defer server.Close()
	go func() { _ = server.Serve(listener) }()
	data, _ := json.Marshal(map[string]any{"url": site, "controller": map[string]any{"token": c.Token, "bootId": "isolated", "capabilities": []string{"sessions.v1", "hls.mse", "timeline.windowed-vod", "video.original", "audio.aac", "audio.select", "text.prepared"}}, "sessionId": a.SessionID})
	if err := os.WriteFile(filepath.Join(directory, "stand.json"), data, 0600); err != nil {
		t.Fatal(err)
	}
	defer os.Remove(filepath.Join(directory, "stand.json"))
	t.Logf("isolated browser review ready; process=%d", os.Getpid())
	for deadline := time.Now().Add(150 * time.Second); time.Now().Before(deadline); time.Sleep(100 * time.Millisecond) {
		m.Tick()
		if _, err := os.Stat(filepath.Join(directory, "stop")); err == nil {
			return
		}
	}
	t.Fatal("browser review deadline")
}
