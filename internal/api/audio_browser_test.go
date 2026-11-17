package api

import (
	"encoding/json"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"

	"yanecompanion/internal/sessions"
)

// Opt-in real Handler/controller/operation/read-grant stand over the generated
// media fixture. Credentials belong only to this temporary test controller.
func serveAudioBrowser(t *testing.T, h Handler, controller sessions.Controller, snapshot sessions.Snapshot) {
	t.Helper()
	directory := os.Getenv("YANE_AUDIO_BROWSER")
	listener, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	server := &http.Server{ReadHeaderTimeout: 2 * time.Second, Handler: h}
	go func() { _ = server.Serve(listener) }()
	defer server.Close()
	if err = os.MkdirAll(directory, 0700); err != nil {
		t.Fatal(err)
	}
	data, _ := json.Marshal(map[string]any{"url": "http://" + listener.Addr().String(), "token": controller.Token,
		"sessionId": snapshot.ID, "fileRef": snapshot.Selected, "origin": website})
	if err = os.WriteFile(filepath.Join(directory, "stand.json"), data, 0600); err != nil {
		t.Fatal(err)
	}
	t.Log("private audio API browser stand ready")
	for deadline := time.Now().Add(5 * time.Minute); time.Now().Before(deadline); {
		if _, err := os.Stat(filepath.Join(directory, "stop")); err == nil {
			return
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Fatal("audio API browser deadline")
}
