package api

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"yanecompanion/internal/config"
	"yanecompanion/internal/engine"
	"yanecompanion/internal/locator"
	"yanecompanion/internal/media"
	"yanecompanion/internal/sessions"
	"yanecompanion/release"
	"yanecompanion/test/fixture"
)

type tracksEngine struct{ mediaEngine }

func (e *tracksEngine) Files(context.Context, string) ([]engine.File, error) {
	files := []engine.File{}
	for i, name := range []string{"episode.mkv", "episode.rus.sdh.srt", "other.rus.srt", "episode.rus.ass"} {
		info, err := os.Stat(filepath.Join(filepath.Dir(e.path), name))
		if err != nil {
			return nil, err
		}
		files = append(files, engine.File{Index: i + 1, Path: name, Length: info.Size()})
	}
	return files, nil
}
func (e *tracksEngine) OpenRange(ctx context.Context, _ string, file engine.File, start, end int64) (io.ReadCloser, error) {
	return (&mediaEngine{path: filepath.Join(filepath.Dir(e.path), filepath.Base(file.Path))}).OpenRange(ctx, "", file, start, end)
}
func tracksHandler(t *testing.T) Handler {
	t.Helper()
	exe := os.Getenv("YANE_TEST_EXECUTABLE")
	if exe == "" {
		t.Skip("requires bundled tools")
	}
	bundle, err := release.Verify(filepath.Dir(exe))
	if err != nil {
		t.Fatal(err)
	}
	e := &tracksEngine{mediaEngine{path: fixture.Tracks(t, bundle.FFmpeg, "../media/testdata/tiny-h264.mp4")}}
	h := pairingHandler(t)
	h.Sessions = sessions.NewWithMedia(e, &media.Service{Runner: media.New(bundle.FFmpeg, bundle.FFprobe), Backend: e, Directory: t.TempDir()})
	h.Pairings.OnRevoke = h.Sessions.Revoke
	t.Cleanup(h.Sessions.Close)
	return h
}

func TestTrackResourceGrantBoundary(t *testing.T) {
	h := tracksHandler(t)
	c, _ := h.Sessions.CreateController(website, "grant")
	key := func(n int) string { return fmt.Sprintf("123e4567-e89b-42d3-a456-%012d", n) }
	a, err := h.Sessions.Create(website, c.Token, key(1), []byte("create"), locator.Locator{Kind: "magnet", Value: "magnet:?xt=urn:btih:" + strings.Repeat("a", 40)})
	if err != nil {
		t.Fatal(err)
	}
	wait := func(state string) sessions.Snapshot {
		t.Helper()
		deadline := time.Now().Add(15 * time.Second)
		for time.Now().Before(deadline) {
			s, _ := h.Sessions.Snapshot(website, c.Token, a.SessionID)
			if s.State == state {
				return s
			}
			if s.Error != "" {
				t.Fatal(s.Error)
			}
			time.Sleep(10 * time.Millisecond)
		}
		t.Fatal("deadline", state)
		return sessions.Snapshot{}
	}
	s := wait("awaiting_file")
	_, err = h.Sessions.Command(website, c.Token, s.ID, key(2), []byte("probe"), sessions.Command{Action: "probe", Revision: s.Revision, FileRef: s.Files[0].Ref})
	if err != nil {
		t.Fatal(err)
	}
	s = wait("awaiting_target")
	firstProbe := s.Probe
	// Production probe disables bitmap/unknown codecs. Their descriptors must
	// remain valid to the player without granting access to any subtitle bytes.
	for i, format := range []string{"pgs", "vobsub", "vtt"} {
		s.Probe.Text = append(s.Probe.Text, media.Text{Ref: fmt.Sprintf("%064x", i+1), Label: "Unavailable", Format: format, Available: false, Reason: "SUBTITLE_CODEC_UNSUPPORTED"})
	}
	target := media.Target{FileRef: s.Selected, AudioRef: s.Probe.Audio[0].Ref, Output: "original"}
	client := []media.ClientMedia{{ContentType: s.Probe.Video.ContentType, Supported: true}, {ContentType: `audio/mp4; codecs="mp4a.40.2"`, Supported: true}}
	_, err = h.Sessions.Command(website, c.Token, s.ID, key(3), []byte("configure"), sessions.Command{Action: "configure", Revision: s.Revision, Target: &target, ClientMedia: client})
	if err != nil {
		t.Fatal(err)
	}
	s = wait("ready")
	if len(s.Source.Text) != 7 || len(s.Source.Text[1].Fonts) != 1 || len(s.Source.Text[3].Fonts) != 1 {
		t.Fatal("missing real resources")
	}
	read := func(method, url, origin, token string) *httptest.ResponseRecorder {
		r := httptest.NewRequest(method, url, nil)
		r.Header.Set("Origin", origin)
		if token != "" {
			r.Header.Set("Authorization", "Bearer "+token)
		}
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		return w
	}
	urls := []string{s.Source.Text[0].URL, s.Source.Text[1].URL, s.Source.Text[2].URL, s.Source.Text[3].URL, s.Source.Text[1].Fonts[0]}
	for _, track := range s.Source.Text[4:] {
		if track.URL == "" {
			t.Fatal("invalid disabled descriptor")
		}
		if w := read("GET", track.URL, website, ""); w.Code != 404 {
			t.Fatal("disabled track became readable", w.Code)
		}
	}
	for _, url := range urls {
		if w := read("HEAD", url, website, ""); w.Code != 200 || w.Body.Len() != 0 {
			t.Fatal("HEAD", w.Code)
		}
		if w := read("GET", url, website, ""); w.Code != 200 {
			t.Fatal("GET", w.Code, w.Body.String())
		}
		for _, tc := range []struct{ origin, token string }{{"https://other.example", ""}, {website, c.Token}, {"", ""}} {
			if w := read("GET", url, tc.origin, tc.token); w.Code == 200 {
				t.Fatal("resource auth bypass")
			}
		}
	}
	old := s.Source.Text[0].URL
	_, err = h.Sessions.Command(website, c.Token, s.ID, key(4), []byte("new probe"), sessions.Command{Action: "probe", Revision: s.Revision, FileRef: s.Selected})
	if err != nil {
		t.Fatal(err)
	}
	if w := read("GET", old, website, ""); w.Code != 404 {
		t.Fatal("old source retained captions")
	}
	s = wait("awaiting_target")
	if s.Probe.Audio[0].Ref == firstProbe.Audio[0].Ref || s.Probe.Text[0].Ref == firstProbe.Text[0].Ref {
		t.Fatal("probe refs survived generation")
	}
	target.TextRef = &firstProbe.Text[0].Ref
	target.AudioRef = s.Probe.Audio[0].Ref
	if _, err = h.Sessions.Command(website, c.Token, s.ID, key(5), []byte("stale text"), sessions.Command{Action: "configure", Revision: s.Revision, Target: &target, ClientMedia: client}); err == nil {
		t.Fatal("stale text ref accepted")
	}
}

// Opt-in browser harness: real API/pairing/FFmpeg/player, deterministic local
// range fixture instead of torrent networking. It never touches the user profile.
func TestLC007Browser(t *testing.T) {
	directory := os.Getenv("YANE_BROWSER_STAND")
	if directory == "" {
		t.Skip("opt-in browser stand")
	}
	h := tracksHandler(t)
	h.DevelopmentOrigin = "http://localhost:5174"
	listener, err := net.Listen("tcp4", config.Address)
	if err != nil {
		t.Fatal(err)
	}
	server := &http.Server{Handler: h, ReadHeaderTimeout: 2 * time.Second, WriteTimeout: 3 * time.Second}
	defer server.Close()
	go server.Serve(listener)
	manage := managementToken(t, h)
	if err := os.MkdirAll(directory, 0700); err != nil {
		t.Fatal(err)
	}
	os.WriteFile(filepath.Join(directory, "manage.url"), []byte("http://"+config.Address+"/local/manage#"+manage), 0600)
	t.Log("LC-007 browser harness ready; file ranges are a deterministic test fixture")
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	deadline := time.NewTimer(25 * time.Minute)
	defer deadline.Stop()
	for {
		select {
		case <-ticker.C:
			h.Sessions.Tick()
			if _, err := os.Stat(filepath.Join(directory, "stop")); err == nil {
				return
			}
		case <-deadline.C:
			return
		}
	}
}
