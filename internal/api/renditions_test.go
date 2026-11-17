package api

import (
	"context"
	"fmt"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"yanecompanion/internal/locator"
	"yanecompanion/internal/media"
	"yanecompanion/internal/sessions"
	"yanecompanion/release"
	"yanecompanion/test/fixture"
)

func TestSeparateAudioSessionContract(t *testing.T) {
	exe := os.Getenv("YANE_TEST_EXECUTABLE")
	if exe == "" {
		t.Skip("requires bundled tools")
	}
	bundle, err := release.Verify(filepath.Dir(exe))
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	runner := media.New(bundle.FFmpeg, bundle.FFprobe)
	path := os.Getenv("YANE_AUDIO_INPUT")
	if path == "" {
		tracks := fixture.Tracks(t, bundle.FFmpeg, "../media/testdata/tiny-h264.mp4")
		path = filepath.Join(t.TempDir(), "separate.mkv")
		_, err = runner.Prepare(ctx, []string{"-v", "error", "-nostdin", "-i", "../media/testdata/tiny-h264.mp4", "-i", tracks, "-map", "0:v:0", "-map", "1:a:0", "-map", "1:a:1", "-c:v", "copy", "-c:a", "pcm_s16le", "-filter:a:0", "atrim=duration=24", "-filter:a:1", "atrim=duration=28", "-t", "28", path})
		if err != nil {
			t.Fatal(err)
		}
	}
	e := &mediaEngine{path: path}
	h := pairingHandler(t)
	h.Sessions = sessions.NewWithMedia(e, &media.Service{Runner: runner, Backend: e, Directory: t.TempDir()})
	t.Cleanup(h.Sessions.Close)
	c, err := h.Sessions.CreateController(website, "grant")
	if err != nil {
		t.Fatal(err)
	}
	key := func(n int) string { return fmt.Sprintf("123e4567-e89b-42d3-a456-%012d", n) }
	a, err := h.Sessions.Create(website, c.Token, key(1), []byte("create"), locator.Locator{Kind: "magnet", Value: "magnet:?xt=urn:btih:" + strings.Repeat("a", 40)})
	if err != nil {
		t.Fatal(err)
	}
	wait := func(state, operation string) sessions.Snapshot {
		t.Helper()
		for deadline := time.Now().Add(20 * time.Second); time.Now().Before(deadline); time.Sleep(10 * time.Millisecond) {
			s, err := h.Sessions.Snapshot(website, c.Token, a.SessionID)
			if err != nil || s.Error != "" {
				t.Fatalf("snapshot %v %s", err, s.Error)
			}
			if operation != "" {
				op, err := h.Sessions.Operation(website, c.Token, operation)
				if err != nil || op.State == "failed" {
					t.Fatalf("operation %+v %v", op, err)
				}
				if op.State != "ready" {
					continue
				}
			}
			if s.State == state {
				return s
			}
		}
		t.Fatal("session deadline")
		return sessions.Snapshot{}
	}
	s := wait("awaiting_file", "")
	_, err = h.Sessions.Command(website, c.Token, s.ID, key(2), []byte("probe"), sessions.Command{Action: "probe", Revision: s.Revision, FileRef: s.Files[0].Ref})
	if err != nil {
		t.Fatal(err)
	}
	s = wait("awaiting_target", key(2))
	target := media.Target{FileRef: s.Selected, AudioRef: s.Probe.Audio[0].Ref, Position: 12, Output: "original", AudioMode: "separate"}
	client := []media.ClientMedia{{ContentType: s.Probe.Video.ContentType, Supported: true}, {ContentType: `audio/mp4; codecs="mp4a.40.2"`, Supported: true}}
	_, err = h.Sessions.Command(website, c.Token, s.ID, key(3), []byte("configure"), sessions.Command{Action: "configure", Revision: s.Revision, Target: &target, ClientMedia: client})
	if err != nil {
		t.Fatal(err)
	}
	s = wait("ready", key(3))
	if os.Getenv("YANE_AUDIO_BROWSER") != "" {
		serveAudioBrowser(t, h, c, s)
		return
	}
	original := *s.Source
	get := func(resource string) *httptest.ResponseRecorder {
		u, _ := url.Parse(original.URL)
		u.Path = strings.TrimSuffix(u.Path, "master.m3u8") + resource
		return call(h, "GET", u.Path, website, "", "")
	}
	master := get("master.m3u8")
	if master.Code != 200 || strings.Count(master.Body.String(), "#EXT-X-MEDIA:") != 2 {
		t.Fatal("master", master.Code, master.Body.String())
	}
	video := get("video-media.m3u8").Body.String()
	old := "audio-" + target.AudioRef + "-" + original.AudioRendition.Generation + "-init.mp4"
	if get(old).Code != 200 {
		t.Fatal("selected audio resources unavailable")
	}
	if get("audio-"+s.Probe.Audio[1].Ref+".m3u8").Code != 404 {
		t.Fatal("unadmitted audio was started by a media read")
	}
	target.AudioRef, target.Position = s.Probe.Audio[1].Ref, 14
	command := sessions.Command{Action: "audio", Revision: s.Revision, Target: &target}
	accepted, err := h.Sessions.Command(website, c.Token, s.ID, key(4), []byte("audio"), command)
	if err != nil {
		t.Fatal(err)
	}
	replay, err := h.Sessions.Command(website, c.Token, s.ID, key(4), []byte("audio"), command)
	if err != nil || replay != accepted {
		t.Fatal("lost-response replay changed admission", err)
	}
	s = wait("ready", key(4))
	if s.Source.ID != original.ID || s.Source.Revision != original.Revision || s.Source.URL != original.URL || s.Source.Timeline.Duration < 27.9 {
		t.Fatal("audio mutated video identity/grant/duration")
	}
	if get(old).Code != 200 {
		t.Fatal("in-flight old audio reads lost their bounded grace cache")
	}
	if get("video-media.m3u8").Body.String() != video {
		t.Fatal("audio selection changed video playlist")
	}
	target.AudioRef = s.Probe.Audio[0].Ref
	_, err = h.Sessions.Command(website, c.Token, s.ID, key(5), []byte("audio back"), sessions.Command{Action: "audio", Revision: s.Revision, Target: &target})
	if err != nil {
		t.Fatal(err)
	}
	s = wait("ready", key(5))
	if get(old).Code != 404 {
		t.Fatal("audio generations accumulated across switches")
	}
	u, _ := url.Parse(original.URL)
	if w := call(h, "GET", u.Path, "https://foreign.example", "", ""); w.Code != 403 {
		t.Fatal("foreign Origin read grant accepted", w.Code)
	}
	if err = h.Sessions.Stop(website, c.Token, s.ID); err != nil {
		t.Fatal(err)
	}
	if get("master.m3u8").Code == 200 {
		t.Fatal("stop retained read grant")
	}
}
