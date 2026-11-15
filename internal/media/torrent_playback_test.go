package media

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"yanecompanion/internal/engine"
	"yanecompanion/internal/locator"
	"yanecompanion/release"
)

// Opt-in real torrent EOF/decoder check; the locator stays in a private input file.
func TestTorrentPlaybackTail(t *testing.T) {
	inputPath, exe := os.Getenv("YANE_PLAYBACK_REVIEW"), os.Getenv("YANE_TEST_EXECUTABLE")
	if inputPath == "" || exe == "" {
		t.Skip("opt-in peer-backed tail playback")
	}
	var spec struct {
		Locator   locator.Locator `json:"locator"`
		FileIndex int             `json:"fileIndex"`
		Positions []float64       `json:"positions"`
	}
	raw, err := os.ReadFile(inputPath)
	if err != nil || json.Unmarshal(raw, &spec) != nil {
		t.Fatal("invalid private input")
	}
	input, err := locator.Validate(spec.Locator)
	if err != nil {
		t.Fatal("invalid locator")
	}
	bundle, err := release.Verify(filepath.Dir(exe))
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Minute)
	defer cancel()
	e, err := engine.Start(ctx, bundle.Engine, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer e.Close()
	if err = e.Admit(ctx, input); err != nil {
		t.Fatal("admission", err)
	}
	var files []engine.File
	for len(files) == 0 && ctx.Err() == nil {
		files, err = e.Files(ctx, input.Hash())
		if err != nil {
			t.Fatal("files", err)
		}
		if len(files) == 0 {
			time.Sleep(250 * time.Millisecond)
		}
	}
	var file engine.File
	for _, candidate := range files {
		if candidate.Index == spec.FileIndex {
			file = candidate
		}
	}
	if file.Length == 0 {
		t.Fatal("missing file")
	}
	runner := New(bundle.FFmpeg, bundle.FFprobe)
	service := &Service{Runner: runner, Backend: e, Directory: t.TempDir()}
	probe, err := service.Probe(ctx, input.Hash(), file)
	if err != nil {
		t.Fatal("probe", err)
	}
	if len(probe.Audio) == 0 {
		t.Fatal("audio missing")
	}
	for _, audio := range []Audio{probe.Audio[0], probe.Audio[len(probe.Audio)-1]} {
		client := []ClientMedia{{probe.Video.ContentType, true}, {`audio/mp4; codecs="mp4a.40.2"`, true}}
		for _, position := range spec.Positions {
			t.Run(fmt.Sprintf("audio=%d/position=%.2f", audio.Index, position), func(t *testing.T) {
				work, stop := context.WithTimeout(ctx, 40*time.Second)
				defer stop()
				began := time.Now()
				p, err := service.Prepare(work, input.Hash(), file, probe, Target{AudioRef: audio.Ref, Position: position, Output: "original"}, client)
				if err != nil {
					t.Fatal("tail prepare", err)
				}
				defer p.Close()
				select {
				case <-p.done:
				case <-work.Done():
					t.Fatal("EOF deadline")
				}
				status := p.Status()
				if status.Error != "" || !status.Timeline.Ended || p.position >= status.Timeline.Duration || status.Timeline.WindowEnd < status.Timeline.Duration-.1 {
					t.Fatalf("invalid EOF %+v", status)
				}
				data, err := p.root.ReadFile("init.mp4")
				if err != nil {
					t.Fatal(err)
				}
				for _, segment := range p.segments {
					fragment, err := p.root.ReadFile(segment.Name)
					if err != nil {
						t.Fatal(err)
					}
					data = append(data, fragment...)
				}
				mux := filepath.Join(t.TempDir(), "tail.mp4")
				if err = os.WriteFile(mux, data, 0600); err != nil {
					t.Fatal(err)
				}
				if _, err = runner.Prepare(work, []string{"-v", "error", "-nostdin", "-i", mux, "-f", "null", "-"}); err != nil {
					t.Fatal("real tail decode", err)
				}
				t.Logf("requested=%.3f selectedDuration=%.6f actualEnd=%.6f ready/decode=%s", position, status.Timeline.Duration, status.Timeline.WindowEnd, time.Since(began).Round(time.Millisecond))
				if audio.Index == probe.Audio[0].Index && position == spec.Positions[0] {
					exportPlaybackFixture(t, p)
				}
			})
		}
	}
}
