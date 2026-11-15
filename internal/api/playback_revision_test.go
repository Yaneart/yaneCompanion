package api

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"yanecompanion/internal/engine"
	"yanecompanion/internal/locator"
	"yanecompanion/internal/media"
	"yanecompanion/internal/sessions"
	"yanecompanion/release"
)

type episodeRevisionEngine struct{ mediaEngine }

func (e *episodeRevisionEngine) Files(context.Context, string) ([]engine.File, error) {
	info, err := os.Stat(e.path)
	if err != nil {
		return nil, err
	}
	return []engine.File{{Index: 1, Path: "S01E01.mp4", Length: info.Size()}, {Index: 2, Path: "S01E02.mp4", Length: info.Size()}}, nil
}

func TestLatePreviousEpisodeProgressIsRejected(t *testing.T) {
	exe := os.Getenv("YANE_TEST_EXECUTABLE")
	if exe == "" {
		t.Skip("requires bundled tools")
	}
	bundle, err := release.Verify(filepath.Dir(exe))
	if err != nil {
		t.Fatal(err)
	}
	e := &episodeRevisionEngine{mediaEngine{path: "../media/testdata/tiny-h264.mp4"}}
	h := pairingHandler(t)
	h.Sessions = sessions.NewWithMedia(e, &media.Service{Runner: media.New(bundle.FFmpeg, bundle.FFprobe), Backend: e, Directory: t.TempDir()})
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
				t.Fatalf("snapshot %v %s", err, s.Error)
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
	var firstRevision int
	var firstID string
	for episode := 0; episode < 2; episode++ {
		_, err = h.Sessions.Command(website, c.Token, s.ID, key(2+episode*2), []byte("probe"), sessions.Command{Action: "probe", Revision: s.Revision, FileRef: s.Files[episode].Ref})
		if err != nil {
			t.Fatal(err)
		}
		s = wait("awaiting_target")
		target := media.Target{FileRef: s.Selected, Position: 0, Output: "original"}
		_, err = h.Sessions.Command(website, c.Token, s.ID, key(3+episode*2), []byte("configure"), sessions.Command{Action: "configure", Revision: s.Revision, Target: &target, ClientMedia: []media.ClientMedia{{ContentType: s.Probe.Video.ContentType, Supported: true}}})
		if err != nil {
			t.Fatal(err)
		}
		s = wait("ready")
		if episode == 0 {
			firstRevision, firstID = s.Source.Revision, s.Source.ID
		}
	}
	if s.Source.ID == firstID {
		t.Fatal("different episode retained its source identity")
	}
	path := "/v1/sessions/" + s.ID + "/progress"
	body := func(revision int) string { return fmt.Sprintf(`{"sourceRevision":%d,"positionSeconds":2}`, revision) }
	if w := call(h, "POST", path, website, c.Token, body(firstRevision)); w.Code != 409 {
		t.Fatalf("late previous episode progress accepted: HTTP%d old revision%d new revision%d", w.Code, firstRevision, s.Source.Revision)
	}
	if w := call(h, "POST", path, website, c.Token, body(s.Source.Revision)); w.Code != 204 {
		t.Fatalf("current progress rejected: HTTP%d", w.Code)
	}
}
