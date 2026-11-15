package api

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
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

func TestSelectedDurationReadyTargetContract(t *testing.T) {
	exe := os.Getenv("YANE_TEST_EXECUTABLE")
	if exe == "" {
		t.Skip("requires bundled tools")
	}
	bundle, err := release.Verify(filepath.Dir(exe))
	if err != nil {
		t.Fatal(err)
	}
	runner := media.New(bundle.FFmpeg, bundle.FFprobe)
	tracks := fixture.Tracks(t, bundle.FFmpeg, "../media/testdata/tiny-h264.mp4")
	path := filepath.Join(t.TempDir(), "selected-24-other-28.mkv")
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	_, err = runner.Prepare(ctx, []string{"-v", "error", "-nostdin", "-i", "../media/testdata/tiny-h264.mp4", "-i", tracks, "-map", "0:v:0", "-map", "1:a:0", "-map", "1:a:1", "-c:v", "copy", "-c:a", "pcm_s16le", "-filter:a:0", "atrim=duration=24", "-filter:a:1", "atrim=duration=28", "-t", "28", path})
	if err != nil {
		t.Fatal("fixture", err)
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
	wait := func(state string) sessions.Snapshot {
		t.Helper()
		deadline := time.Now().Add(15 * time.Second)
		for time.Now().Before(deadline) {
			s, err := h.Sessions.Snapshot(website, c.Token, a.SessionID)
			if err != nil || s.Error != "" {
				t.Fatalf("snapshot: %v %s", err, s.Error)
			}
			if s.State == state {
				return s
			}
			time.Sleep(10 * time.Millisecond)
		}
		t.Fatalf("deadline waiting for %s", state)
		return sessions.Snapshot{}
	}
	s := wait("awaiting_file")
	_, err = h.Sessions.Command(website, c.Token, s.ID, key(2), []byte("probe"), sessions.Command{Action: "probe", Revision: s.Revision, FileRef: s.Files[0].Ref})
	if err != nil {
		t.Fatal(err)
	}
	s = wait("awaiting_target")
	if math.Abs(s.Probe.Duration-28) > .1 || len(s.Probe.Audio) != 2 {
		t.Fatal("fixture must expose the longer unselected audio")
	}
	target := media.Target{FileRef: s.Selected, AudioRef: s.Probe.Audio[0].Ref, Position: s.Probe.Duration - .2, Output: "original"}
	client := []media.ClientMedia{{ContentType: s.Probe.Video.ContentType, Supported: true}, {ContentType: `audio/mp4; codecs="mp4a.40.2"`, Supported: true}}
	for attempt, audioIndex := range []int{0, 1, 0} {
		target.AudioRef = s.Probe.Audio[audioIndex].Ref
		wantDuration := 24.0
		wantPosition := 23.99
		if audioIndex == 1 {
			wantDuration = 28
		}
		_, err = h.Sessions.Command(website, c.Token, s.ID, key(3+attempt), []byte("configure"), sessions.Command{Action: "configure", Revision: s.Revision, Target: &target, ClientMedia: client})
		if err != nil {
			t.Fatal(err)
		}
		wait("ready")
		w := call(h, "GET", "/v1/sessions/"+s.ID, website, c.Token, "")
		if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &s) != nil {
			t.Fatal("ready snapshot response", w.Code)
		}
		var wire struct {
			Source struct {
				Timeline struct {
					VideoEnd float64 `json:"videoEndSeconds"`
				} `json:"timeline"`
			} `json:"source"`
		}
		if json.Unmarshal(w.Body.Bytes(), &wire) != nil || math.Abs(wire.Source.Timeline.VideoEnd-24) > .1 {
			t.Fatal("ready snapshot lost the actual video seek boundary")
		}
		if s.Source == nil || s.Target == nil || math.Abs(s.Source.Timeline.Duration-wantDuration) > .1 || math.Abs(s.Target.Position-wantPosition) > .01 {
			t.Fatalf("ready target escaped the selected timeline: %+v %+v", s.Target, s.Source)
		}
		if s.Source.Revision != attempt+1 || len(s.Source.Ranges) == 0 || s.Source.Timeline.WindowEnd < s.Target.Position {
			t.Fatal("ready source does not cover its clamped target")
		}
	}
}
