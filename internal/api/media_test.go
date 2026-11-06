package api

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
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
)

type mediaEngine struct {
	path    string
	gate    <-chan struct{}
	started chan struct{}
}

func (e *mediaEngine) Admit(context.Context, locator.Input) error { return nil }
func (e *mediaEngine) Files(context.Context, string) ([]engine.File, error) {
	info, _ := os.Stat(e.path)
	return []engine.File{{Index: 1, Path: "fixture.mp4", Length: info.Size()}}, nil
}
func (e *mediaEngine) Download(context.Context, string, engine.File) (int64, error) { return 0, nil }
func (e *mediaEngine) Drop(context.Context, string) error                           { return nil }
func (e *mediaEngine) OpenRange(ctx context.Context, _ string, _ engine.File, start, end int64) (io.ReadCloser, error) {
	if e.gate != nil {
		if e.started != nil {
			select {
			case e.started <- struct{}{}:
			default:
			}
		}
		select {
		case <-e.gate:
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	file, err := os.Open(e.path)
	if err != nil {
		return nil, err
	}
	return &mediaSection{io.NewSectionReader(file, start, end-start+1), file}, nil
}

func TestCancelPendingProbeRetainsSessionAndRejectsLatePublication(t *testing.T) {
	executable := os.Getenv("YANE_TEST_EXECUTABLE")
	if executable == "" {
		t.Skip("requires bundled media")
	}
	bundle, err := release.Verify(filepath.Dir(executable))
	if err != nil {
		t.Fatal(err)
	}
	gate := make(chan struct{})
	started := make(chan struct{}, 1)
	e := &mediaEngine{path: "../media/testdata/tiny-h264.mp4", gate: gate, started: started}
	m := sessions.NewWithMedia(e, &media.Service{Runner: media.New(bundle.FFmpeg, bundle.FFprobe), Backend: e, Directory: t.TempDir()})
	defer m.Close()
	c, _ := m.CreateController(website, "grant")
	key := "123e4567-e89b-42d3-a456-426614174000"
	commandKey := "123e4567-e89b-42d3-a456-426614174001"
	a, err := m.Create(website, c.Token, key, []byte("create"), locator.Locator{Kind: "magnet", Value: "magnet:?xt=urn:btih:" + strings.Repeat("a", 40)})
	if err != nil {
		t.Fatal(err)
	}
	var s sessions.Snapshot
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		s, _ = m.Snapshot(website, c.Token, a.SessionID)
		if s.State == "awaiting_file" {
			break
		}
		time.Sleep(time.Millisecond)
	}
	cmd := sessions.Command{Action: "probe", Revision: s.Revision, FileRef: s.Files[0].Ref}
	a, err = m.Command(website, c.Token, s.ID, commandKey, []byte("probe"), cmd)
	if err != nil {
		t.Fatal(err)
	}
	select {
	case <-started:
	case <-time.After(5 * time.Second):
		t.Fatal("probe did not start")
	}
	if err = m.Cancel(website, c.Token, commandKey, a.Revision); err != nil {
		t.Fatal(err)
	}
	close(gate)
	time.Sleep(100 * time.Millisecond)
	s, _ = m.Snapshot(website, c.Token, s.ID)
	op, _ := m.Operation(website, c.Token, commandKey)
	if s.State != "awaiting_file" || s.Revision != a.Revision+1 || s.Probe != nil || s.Source != nil || op.State != "cancelled" {
		t.Fatalf("cancelled probe published late: %+v %+v", s, op)
	}
	if replay, err := m.Command(website, c.Token, s.ID, commandKey, []byte("probe"), cmd); err != nil || replay != a {
		t.Fatal("cancelled replay retried work", err)
	}
}

type mediaSection struct {
	*io.SectionReader
	io.Closer
}

func TestRealMediaCommandsAndReadGrant(t *testing.T) {
	executable := os.Getenv("YANE_TEST_EXECUTABLE")
	if executable == "" {
		t.Skip("requires bundled FFmpeg/ffprobe")
	}
	bundle, err := release.Verify(filepath.Dir(executable))
	if err != nil {
		t.Fatal(err)
	}
	h := pairingHandler(t)
	e := &mediaEngine{path: "../media/testdata/tiny-h264.mp4"}
	h.Sessions = sessions.NewWithMedia(e, &media.Service{Runner: media.New(bundle.FFmpeg, bundle.FFprobe), Backend: e, Directory: t.TempDir()})
	defer h.Sessions.Close()
	h.Pairings.OnRevoke = h.Sessions.Revoke
	request := create(t, h)
	if err := h.Pairings.Decide(request.RequestID, request.Code, true); err != nil {
		t.Fatal(err)
	}
	paired, _ := h.Pairings.Poll(website, request.RequestID, request.PollToken)
	var controller sessions.Controller
	w := call(h, "POST", "/v1/controllers", website, paired.PairingToken, "{}")
	_ = json.Unmarshal(w.Body.Bytes(), &controller)
	post := func(path, key, body string) *httptest.ResponseRecorder {
		r := httptest.NewRequest("POST", "http://"+config.Address+path, strings.NewReader(body))
		r.Header.Set("Origin", website)
		r.Header.Set("Authorization", "Bearer "+controller.Token)
		r.Header.Set("Content-Type", "application/json")
		r.Header.Set("Idempotency-Key", key)
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		return w
	}
	key := func(n int) string { return fmt.Sprintf("123e4567-e89b-42d3-a456-%012d", n) }
	w = post("/v1/sessions", key(1), `{"locator":{"kind":"magnet","value":"magnet:?xt=urn:btih:`+strings.Repeat("a", 40)+`"}}`)
	if w.Code != 202 {
		t.Fatal(w.Body.String())
	}
	var admission sessions.Admission
	_ = json.Unmarshal(w.Body.Bytes(), &admission)
	snapshot := func(state string) sessions.Snapshot {
		deadline := time.Now().Add(15 * time.Second)
		for time.Now().Before(deadline) {
			s, err := h.Sessions.Snapshot(website, controller.Token, admission.SessionID)
			if err != nil {
				t.Fatal(err)
			}
			if s.State == state {
				return s
			}
			if s.Error != "" {
				t.Fatalf("failed snapshot: %+v", s)
			}
			time.Sleep(20 * time.Millisecond)
		}
		t.Fatal("state deadline", state)
		return sessions.Snapshot{}
	}
	s := snapshot("awaiting_file")
	commandPath := "/v1/sessions/" + s.ID + "/commands"
	probeBody := fmt.Sprintf(`{"action":"probe","expectedRevision":1,"fileRef":%q}`, s.Files[0].Ref)
	w = post(commandPath, key(2), probeBody)
	if w.Code != 202 {
		t.Fatal(w.Body.String())
	}
	s = snapshot("awaiting_target")
	if s.Probe == nil || s.Source != nil {
		t.Fatal("probe produced media or no metadata")
	}
	if replay := post(commandPath, key(2), probeBody); replay.Code != 202 {
		t.Fatal("probe replay did not precede CAS")
	}
	configure := fmt.Sprintf(`{"action":"configure","expectedRevision":%d,"target":{"fileRef":%q,"audioRef":"","textRef":null,"positionSeconds":0,"output":"original"},"clientMedia":[{"contentType":%q,"supported":true}]}`, s.Revision, s.Selected, s.Probe.Video.ContentType)
	if w = post(commandPath, key(3), configure); w.Code != 202 {
		t.Fatal(w.Body.String())
	}
	s = snapshot("ready")
	oldSource := s.Source
	if oldSource == nil || !strings.HasPrefix(oldSource.URL, "http://"+config.Address+"/v1/media/") {
		t.Fatal("missing private read grant")
	}
	read := func(method, path, origin, token, rangeHeader string) *httptest.ResponseRecorder {
		r := httptest.NewRequest(method, path, nil)
		r.Header.Set("Origin", origin)
		if token != "" {
			r.Header.Set("Authorization", "Bearer "+token)
		}
		if rangeHeader != "" {
			r.Header.Set("Range", rangeHeader)
		}
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		return w
	}
	for _, tc := range []struct {
		origin, token, rangeHeader string
		status                     int
	}{{website, "", "", 200}, {"https://other.example", "", "", 404}, {"", "", "", 403}, {website, controller.Token, "", 404}, {website, paired.PairingToken, "", 404}, {website, "", "bytes=0-1", 416}} {
		w = read("GET", oldSource.URL, tc.origin, tc.token, tc.rangeHeader)
		if w.Code != tc.status {
			t.Fatalf("media authorization: got %d want %d", w.Code, tc.status)
		}
	}
	for _, suffix := range []string{"../accs.db", "%2e%2e/accs.db", "init.mp4?x=1", "missing.mp4"} {
		url := strings.TrimSuffix(oldSource.URL, "master.m3u8") + suffix
		if w := read("GET", url, website, "", ""); w.Code == 200 {
			t.Fatal("arbitrary resource allowed", suffix)
		}
	}
	if w := read("HEAD", oldSource.URL, website, "", ""); w.Code != 200 || w.Body.Len() != 0 {
		t.Fatal("HEAD fetched bytes")
	}
	if err := h.Sessions.Progress(website, controller.Token, s.ID, oldSource.Revision+1, 1); err == nil {
		t.Fatal("stale progress changed cache")
	}
	// A replacement immediately revokes the old grant, even before new ready.
	configure = strings.Replace(configure, `"expectedRevision":2`, `"expectedRevision":3`, 1)
	if w = post(commandPath, key(4), configure); w.Code != 202 {
		t.Fatal("replacement", w.Body.String())
	}
	if w := read("GET", oldSource.URL, website, "", ""); w.Code != 404 {
		t.Fatal("replacement retained old grant")
	}
	s = snapshot("ready")
	if s.Source.ID != oldSource.ID || s.Source.Revision != oldSource.Revision+1 || s.Source.URL == oldSource.URL {
		t.Fatal("source identity/revision did not advance")
	}
	if w := call(h, "DELETE", "/v1/pairings/current", website, paired.PairingToken, ""); w.Code != 204 {
		t.Fatal("revoke", w.Code)
	}
	if w := read("GET", s.Source.URL, website, "", ""); w.Code != 404 {
		t.Fatal("revoke retained media grant")
	}
}
