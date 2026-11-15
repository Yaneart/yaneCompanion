package media

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"yanecompanion/internal/engine"
	"yanecompanion/release"
)

func TestRealPlaybackExcludesChapterTracks(t *testing.T) {
	executable := os.Getenv("YANE_TEST_EXECUTABLE")
	if executable == "" {
		t.Skip("requires bundled media executables")
	}
	bundle, err := release.Verify(filepath.Dir(executable))
	if err != nil {
		t.Fatal(err)
	}
	runner := New(bundle.FFmpeg, bundle.FFprobe)
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	directory := t.TempDir()
	metadata := filepath.Join(directory, "chapters.txt")
	if err := os.WriteFile(metadata, []byte(";FFMETADATA1\n[CHAPTER]\nTIMEBASE=1/1000\nSTART=0\nEND=60000\ntitle=First\n[CHAPTER]\nTIMEBASE=1/1000\nSTART=60000\nEND=96000\ntitle=Second\n"), 0600); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(directory, "video with chapters.mkv")
	_, err = runner.Prepare(ctx, []string{"-v", "error", "-nostdin", "-stream_loop", "3", "-i", "testdata/tiny-h264.mp4", "-stream_loop", "-1", "-i", waveFixture(t), "-f", "ffmetadata", "-i", metadata, "-map", "0:v:0", "-map", "1:a:0", "-map_chapters", "2", "-c:v", "copy", "-c:a", "pcm_s16le", "-t", "96", path})
	if err != nil {
		t.Fatal("fixture", err)
	}
	data, err := runner.Probe(ctx, []string{"-v", "error", "-show_chapters", "-of", "json", path})
	var source struct {
		Chapters []json.RawMessage `json:"chapters"`
	}
	if err != nil || json.Unmarshal(data, &source) != nil || len(source.Chapters) != 2 {
		t.Fatal("fixture must contain chapters", err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	file := engine.File{Index: 1, Path: "video.mkv", Length: info.Size()}
	service := &Service{Runner: runner, Backend: fileRanges{path}, Directory: t.TempDir()}
	probe, err := service.Probe(ctx, "fixture", file)
	if err != nil {
		t.Fatal("probe", err)
	}
	if len(probe.Audio) != 1 {
		t.Fatal("fixture audio missing")
	}
	client := []ClientMedia{{probe.Video.ContentType, true}, {`audio/mp4; codecs="mp4a.40.2"`, true}}
	for _, position := range []float64{0, 63.3} {
		playback, err := service.Prepare(ctx, "fixture", file, probe, Target{AudioRef: probe.Audio[0].Ref, Output: "original", Position: position}, client)
		if err != nil {
			t.Fatalf("prepare at %.1f: %v", position, err)
		}
		status := playback.Status()
		init, err := playback.root.ReadFile("init.mp4")
		clock, clockErr := initClock(init)
		playback.Close()
		if err != nil || clockErr != nil || len(clock.timescales) != 2 {
			t.Fatalf("output must contain only video and audio, without an implicit chapter track: %v %v tracks=%d", err, clockErr, len(clock.timescales))
		}
		if status.Error != "" || status.Bytes == 0 || status.Timeline.WindowEnd <= position || status.Output.Video.Mode != "copy" || status.Output.Audio == nil || status.Output.Audio.Mode != "transcode" {
			t.Fatalf("chaptered media did not prepare a measured A/V prefix at %.1f: %+v", position, status)
		}
	}
}
