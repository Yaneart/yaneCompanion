package media

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"yanecompanion/internal/engine"
	"yanecompanion/release"
)

func TestRealAudioConversionKeepsVideo(t *testing.T) {
	executable := os.Getenv("YANE_TEST_EXECUTABLE")
	if executable == "" {
		t.Skip("requires bundled media")
	}
	bundle, err := release.Verify(filepath.Dir(executable))
	if err != nil {
		t.Fatal(err)
	}
	runner := New(bundle.FFmpeg, bundle.FFprobe)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	path := filepath.Join(t.TempDir(), "video and pcm.mkv")
	_, err = runner.Prepare(ctx, []string{"-v", "error", "-nostdin", "-i", "testdata/tiny-h264.mp4", "-stream_loop", "-1", "-i", waveFixture(t), "-map", "0:v:0", "-map", "1:a:0", "-c:v", "copy", "-c:a", "pcm_s16le", "-t", "24", path})
	if err != nil {
		t.Fatal("fixture", err)
	}
	info, _ := os.Stat(path)
	service := &Service{Runner: runner, Backend: fileRanges{path}, Directory: t.TempDir()}
	file := engine.File{Index: 1, Path: "fixture.mkv", Length: info.Size()}
	probe, err := service.Probe(ctx, "hash", file)
	if err != nil {
		t.Fatal(err)
	}
	if len(probe.Audio) != 1 || probe.Audio[0].Codec != "pcm_s16le" {
		t.Fatal("missing PCM track")
	}
	p, err := service.Prepare(ctx, "hash", file, probe, Target{AudioRef: probe.Audio[0].Ref, Output: "original"}, []ClientMedia{{probe.Video.ContentType, true}, {`audio/mp4; codecs="mp4a.40.2"`, true}})
	if err != nil {
		t.Fatal(err)
	}
	status := p.Status()
	if status.Output.Video.ContentType != probe.Video.ContentType || status.Output.Audio.Codec != "aac" || status.Output.Audio.Mode != "transcode" {
		p.Close()
		t.Fatalf("wrong independent audio output %+v", status.Output)
	}
	p.mu.Lock()
	data, err := p.root.ReadFile("init.mp4")
	if err != nil {
		p.mu.Unlock()
		p.Close()
		t.Fatal(err)
	}
	for _, item := range p.segments {
		fragment, err := p.root.ReadFile(item.Name)
		if err != nil {
			p.mu.Unlock()
			p.Close()
			t.Fatal(err)
		}
		data = append(data, fragment...)
	}
	p.mu.Unlock()
	p.Close()
	prepared := filepath.Join(t.TempDir(), "prepared.mp4")
	if err = os.WriteFile(prepared, data, 0600); err != nil {
		t.Fatal(err)
	}
	actual, err := runner.Probe(ctx, []string{"-v", "error", "-show_streams", "-show_format", "-show_data", "-of", "json", prepared})
	if err != nil {
		t.Fatal(err)
	}
	parsed, err := parseProbe(actual)
	if err != nil || len(parsed.Audio) != 1 || parsed.Audio[0].Codec != "aac" || parsed.Video.ContentType != probe.Video.ContentType {
		t.Fatal("prepared video changed or AAC missing", err)
	}
	if _, err = runner.Prepare(ctx, []string{"-v", "error", "-nostdin", "-i", prepared, "-t", "2", "-f", "null", "-"}); err != nil {
		t.Fatal("converted audio/video decode", err)
	}
}
