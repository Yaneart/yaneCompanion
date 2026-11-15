package media

import (
	"context"
	"encoding/json"
	"math"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"yanecompanion/internal/engine"
	"yanecompanion/release"
)

func TestRealSelectedTracksDuration(t *testing.T) {
	exe := os.Getenv("YANE_TEST_EXECUTABLE")
	if exe == "" {
		t.Skip("requires bundled media")
	}
	bundle, err := release.Verify(filepath.Dir(exe))
	if err != nil {
		t.Fatal(err)
	}
	runner := New(bundle.FFmpeg, bundle.FFprobe)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	path := filepath.Join(t.TempDir(), "different-track-ends.mkv")
	wave := waveFixture(t)
	_, err = runner.Prepare(ctx, []string{"-v", "error", "-nostdin", "-i", "testdata/tiny-h264.mp4", "-stream_loop", "-1", "-i", wave, "-stream_loop", "-1", "-i", wave, "-map", "0:v:0", "-map", "1:a:0", "-map", "2:a:0", "-c:v", "copy", "-c:a", "pcm_s16le", "-filter:a:0", "atrim=duration=24", "-t", "28", path})
	if err != nil {
		t.Fatal("fixture", err)
	}
	info, _ := os.Stat(path)
	file := engine.File{Index: 1, Path: "fixture.mkv", Length: info.Size()}
	service := &Service{Runner: runner, Backend: fileRanges{path}, Directory: t.TempDir()}
	probe, err := service.Probe(ctx, "hash", file)
	if err != nil {
		t.Fatal("probe", err)
	}
	if probe.Duration < 27.9 || len(probe.Audio) != 2 || math.Abs(probe.playbackDuration(&probe.Audio[0])-24) > .1 || probe.playbackDuration(&probe.Audio[1]) < 27.9 {
		t.Fatalf("lost independent track durations: total=%f video=%f audio=%+v", probe.Duration, probe.videoEnd, probe.Audio)
	}
	client := []ClientMedia{{probe.Video.ContentType, true}, {`audio/mp4; codecs="mp4a.40.2"`, true}}
	for attempt := 0; attempt < 4; attempt++ {
		p, err := service.Prepare(ctx, "hash", file, probe, Target{AudioRef: probe.Audio[0].Ref, Position: probe.Duration - .2, Output: "original"}, client)
		if err != nil {
			t.Fatal("tail prepare", err)
		}
		status := p.Status()
		if status.Error != "" || math.Abs(status.Timeline.Duration-24) > .1 || status.Timeline.WindowEnd < 23.9 || p.position >= status.Timeline.Duration {
			p.Close()
			t.Fatalf("selected stream EOF not ready: %+v", status)
		}
		p.Close()
	}
	long, err := service.Prepare(ctx, "hash", file, probe, Target{AudioRef: probe.Audio[1].Ref, Position: 27.8, Output: "original"}, client)
	if err != nil {
		t.Fatal("longer selected audio tail", err)
	}
	defer long.Close()
	select {
	case <-long.done:
	case <-ctx.Done():
		t.Fatal("longer audio EOF deadline")
	}
	status := long.Status()
	if status.Error != "" || !status.Timeline.Ended || status.Timeline.Duration < 27.9 || status.Timeline.WindowEnd < 27.9 || long.position >= status.Timeline.VideoEnd {
		t.Fatalf("longer selected audio lost: %+v", status)
	}
	data, err := long.root.ReadFile("init.mp4")
	if err != nil {
		t.Fatal(err)
	}
	clock, err := initClock(data)
	if err != nil {
		t.Fatal("long tail init clocks", err)
	}
	for _, scale := range clock.timescales {
		if scale != clock.timescales[clock.video] {
			t.Fatal("AV timescales would reset the HLS offset at the final audio-only fragment")
		}
	}
	for _, segment := range long.segments {
		fragment, err := long.root.ReadFile(segment.Name)
		if err != nil {
			t.Fatal(err)
		}
		data = append(data, fragment...)
	}
	output := filepath.Join(t.TempDir(), "long-audio-tail.mp4")
	if err = os.WriteFile(output, data, 0600); err != nil {
		t.Fatal(err)
	}
	if _, err = runner.Prepare(ctx, []string{"-v", "error", "-nostdin", "-i", output, "-f", "null", "-"}); err != nil {
		t.Fatal("long tail real decoder", err)
	}
	exportPlaybackFixture(t, long)
}

func exportPlaybackFixture(t *testing.T, long *Playback) {
	t.Helper()
	var err error
	if artifacts := os.Getenv("YANE_PLAYBACK_ARTIFACTS"); artifacts != "" {
		if !filepath.IsAbs(artifacts) {
			t.Fatal("artifact directory must be absolute")
		}
		if err = os.MkdirAll(artifacts, 0700); err != nil {
			t.Fatal(err)
		}
		state, _ := json.Marshal(long.Status())
		if err = os.WriteFile(filepath.Join(artifacts, "status.json"), state, 0600); err != nil {
			t.Fatal(err)
		}
		names := []string{"init.mp4"}
		for _, segment := range long.segments {
			names = append(names, segment.Name)
		}
		for _, name := range names {
			data, err := long.root.ReadFile(name)
			if err != nil {
				t.Fatal(err)
			}
			if err = os.WriteFile(filepath.Join(artifacts, name), data, 0600); err != nil {
				t.Fatal(err)
			}
		}
		w := httptest.NewRecorder()
		long.Serve(w, httptest.NewRequest("GET", "http://local/media.m3u8", nil), "media.m3u8")
		if w.Code != 200 {
			t.Fatal("fixture manifest", w.Code)
		}
		if err = os.WriteFile(filepath.Join(artifacts, "media.m3u8"), w.Body.Bytes(), 0600); err != nil {
			t.Fatal(err)
		}
	}
}

func TestTrackEndMetadataValidation(t *testing.T) {
	for _, tc := range []struct {
		tag, duration, start string
		offset, total, want  float64
	}{
		{"01:56:22.976000000", "", "0", 0, 6987.52, 6982.976},
		{"00:00:26.000", "", "2", 2, 30, 24},
		{"", "24", "2", 2, 30, 24},
		{"", "24", "3", 2, 30, 25},
		{"00:00:24.000", "", "0", 0, 24, 24},
		{"00:00:60.0", "", "0", 0, 100, 0},
		{"00:60:00.0", "", "0", 0, 10000, 0},
		{"00:00:NaN", "", "0", 0, 100, 0},
		{"", "NaN", "0", 0, 100, 0},
		{"00:01:00.0", "", "0", 0, 24, 0},
		{"garbage", "", "0", 0, 100, 0},
	} {
		st := rawStream{Start: tc.start, Duration: tc.duration}
		st.Tags.Duration = tc.tag
		if got := streamEnd(st, tc.offset, tc.total); math.Abs(got-tc.want) > 1e-6 {
			t.Fatalf("tag=%q duration=%q start=%q: got %f want %f", tc.tag, tc.duration, tc.start, got, tc.want)
		}
	}
	p := &Probe{Duration: 30, videoEnd: 24}
	if p.playbackDuration(&Audio{}) != 30 || p.playbackDuration(nil) != 24 || p.playbackDuration(&Audio{end: 28}) != 28 {
		t.Fatal("unknown selected audio duration must retain the container bound")
	}
}

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
