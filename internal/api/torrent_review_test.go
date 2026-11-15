package api

import (
	"context"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"yanecompanion/internal/config"
	"yanecompanion/internal/engine"
	"yanecompanion/internal/media"
	"yanecompanion/internal/sessions"
	"yanecompanion/release"
)

type reviewEngine struct {
	*engine.Engine
	reads, bytes, active atomic.Int64
}

type reviewReader struct {
	io.ReadCloser
	owner *reviewEngine
}

func (r *reviewReader) Read(p []byte) (int, error) {
	n, err := r.ReadCloser.Read(p)
	r.owner.bytes.Add(int64(n))
	return n, err
}

func (r *reviewReader) Close() error {
	r.owner.active.Add(-1)
	return r.ReadCloser.Close()
}

func (e *reviewEngine) OpenRange(ctx context.Context, hash string, file engine.File, start, end int64) (io.ReadCloser, error) {
	e.reads.Add(1)
	r, err := e.Engine.OpenRange(ctx, hash, file, start, end)
	if err != nil {
		return nil, err
	}
	e.active.Add(1)
	return &reviewReader{r, e}, nil
}

// Opt-in peer-backed browser stand. It uses an independent engine/profile and
// never binds the normal Companion port. Only the isolated browser context
// forwards its requests here, preserving the API's canonical Host and Origin.
// No mocked torrent data, codec output or playback responses are used.
func TestTorrentReviewBrowser(t *testing.T) {
	directory := os.Getenv("YANE_TORRENT_REVIEW")
	if directory == "" {
		t.Skip("opt-in real torrent browser review")
	}
	exe := os.Getenv("YANE_TEST_EXECUTABLE")
	if !filepath.IsAbs(directory) || !filepath.IsAbs(exe) {
		t.Fatal("requires absolute scratch and bundle paths")
	}
	bundle, err := release.Verify(filepath.Dir(exe))
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	worker, err := engine.Start(ctx, bundle.Engine, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer worker.Close()
	e := &reviewEngine{Engine: worker}
	h := pairingHandler(t)
	h.DevelopmentOrigin = "http://localhost:5174"
	h.Sessions = sessions.NewWithMedia(e, &media.Service{Runner: media.New(bundle.FFmpeg, bundle.FFprobe), Backend: e, Directory: t.TempDir()})
	defer h.Sessions.Close()
	h.Pairings.OnRevoke = h.Sessions.Revoke
	listener, err := net.Listen("tcp4", "127.0.0.1:45724")
	if err != nil {
		t.Fatal(err)
	}
	server := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		r.Host = config.Address
		h.ServeHTTP(w, r)
	}), ReadHeaderTimeout: 2 * time.Second, WriteTimeout: 3 * time.Second}
	defer server.Close()
	go server.Serve(listener)
	if err := os.MkdirAll(directory, 0700); err != nil {
		t.Fatal(err)
	}
	os.WriteFile(filepath.Join(directory, "management-token"), []byte(managementToken(t, h)), 0600)
	t.Log("real torrent stand ready on private port 45724")
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	deadline := time.NewTimer(55 * time.Minute)
	defer deadline.Stop()
	for {
		select {
		case <-ticker.C:
			h.Sessions.Tick()
			if _, err := os.Stat(filepath.Join(directory, "refresh-management")); err == nil {
				os.WriteFile(filepath.Join(directory, "management-token"), []byte(managementToken(t, h)), 0600)
				os.Remove(filepath.Join(directory, "refresh-management"))
			}
			if _, err := os.Stat(filepath.Join(directory, "stop")); err == nil {
				t.Logf("range reads=%d bytes=%d active=%d", e.reads.Load(), e.bytes.Load(), e.active.Load())
				return
			}
		case <-deadline.C:
			return
		}
	}
}
