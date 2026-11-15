package media

import (
	"context"
	"io"
	"math"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"yanecompanion/internal/engine"
	"yanecompanion/release"
	"yanecompanion/test/fixture"
)

type siblingRanges struct{ directory string }

type heldRanges struct{ started chan struct{} }

func (h heldRanges) OpenRange(ctx context.Context, _ string, _ engine.File, _, _ int64) (io.ReadCloser, error) {
	select {
	case h.started <- struct{}{}:
	default:
	}
	<-ctx.Done()
	return nil, ctx.Err()
}

func (s siblingRanges) OpenRange(ctx context.Context, _ string, file engine.File, start, end int64) (io.ReadCloser, error) {
	return fileRanges{filepath.Join(s.directory, filepath.Base(file.Path))}.OpenRange(ctx, "", file, start, end)
}

func TestCaptionPolicy(t *testing.T) {
	p := &Probe{}
	p.AddSidecars(engine.File{Path: "dir/episode.mkv"}, []engine.File{{Path: "dir/episode.rus.forced.sdh.srt", Length: 40}, {Path: "dir/episode2.srt", Length: 40}, {Path: "other/episode.srt", Length: 40}, {Path: "dir/episode.notes.srt", Length: 40}, {Path: "dir/episode.ass", Length: textLimit + 1}})
	if len(p.Text) != 1 || p.Text[0].Language != "rus" || !p.Text[0].Forced || !p.Text[0].SDH {
		t.Fatalf("unsafe matching %+v", p.Text)
	}
	for _, stamp := range []string{"nonsense", "00:99:00.000", "00:00:NaN"} {
		if finite(cueTime(stamp)) {
			t.Fatal("invalid time", stamp)
		}
	}
	if _, err := subtitleWindow([]byte("WEBVTT\n\n00:00:00.000 --> 00:03:00.000\nlong\n"), "vtt", 0, 60); err == nil {
		t.Fatal("unbounded cue")
	}
	data, err := subtitleWindow([]byte("[Script Info]\nTitle: Test\n[Fonts]\nfontname: private.ttf\nsecret\n[Events]\nDialogue: 0,0:00:10.00,0:00:12.00,Default,,0,0,0,,visible\n"), "ass", 5, 20)
	if err != nil || strings.Contains(string(data), "secret") || !strings.Contains(string(data), "visible") {
		t.Fatal("ASS resource section escaped", err)
	}
}

func TestRealTracksAndCaptions(t *testing.T) {
	exe := os.Getenv("YANE_TEST_EXECUTABLE")
	if exe == "" {
		t.Skip("requires bundled tools")
	}
	bundle, err := release.Verify(filepath.Dir(exe))
	if err != nil {
		t.Fatal(err)
	}
	path := fixture.Tracks(t, bundle.FFmpeg, "testdata/tiny-h264.mp4")
	info, _ := os.Stat(path)
	file := engine.File{Index: 1, Path: "episode.mkv", Length: info.Size()}
	service := &Service{Runner: New(bundle.FFmpeg, bundle.FFprobe), Backend: siblingRanges{filepath.Dir(path)}, Directory: t.TempDir()}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	probe, err := service.Probe(ctx, "hash", file)
	if err != nil {
		t.Fatal("probe", err)
	}
	sideInfo, _ := os.Stat(filepath.Join(filepath.Dir(path), "episode.rus.sdh.srt"))
	assInfo, _ := os.Stat(filepath.Join(filepath.Dir(path), "episode.rus.ass"))
	probe.AddSidecars(file, []engine.File{{Index: 2, Path: "episode.rus.sdh.srt", Length: sideInfo.Size()}, {Index: 3, Path: "other.rus.srt", Length: sideInfo.Size()}, {Index: 4, Path: "episode.rus.ass", Length: assInfo.Size()}})
	if len(probe.Audio) != 2 || len(probe.Text) != 4 || len(probe.Fonts) != 1 || probe.Audio[1].Language != "rus" || !probe.Text[0].Forced || !probe.Text[0].SDH {
		t.Fatalf("metadata %+v", probe)
	}
	client := []ClientMedia{{probe.Video.ContentType, true}, {`audio/mp4; codecs="mp4a.40.2"`, true}}
	prepare := func(audio int, position float64) *Playback {
		t.Helper()
		p, err := service.Prepare(ctx, "hash", file, probe, Target{AudioRef: probe.Audio[audio].Ref, TextRef: &probe.Text[1].Ref, Output: "original", Position: position}, client)
		if err != nil {
			t.Fatal("prepare", err)
		}
		return p
	}
	p := prepare(0, 0)
	t.Cleanup(p.Close)
	for _, tc := range []struct {
		position, delay float64
		content         string
	}{{0, -60, "TEXT 030"}, {190, 60, "LONG DELAY"}, {10, 60, "WEBVTT"}, {191, -60, "WEBVTT"}} {
		p.captions.delay = &tc.delay
		data, start, end, err := p.extractResource(ctx, probe.Text[0], nil, tc.position)
		if err != nil || !strings.Contains(string(data), tc.content) || start != tc.position || end != math.Min(probe.Duration, tc.position+30) {
			t.Fatalf("window position=%g delay=%g: %g..%g %v %s", tc.position, tc.delay, start, end, err, data)
		}
	}
	p.captions.delay = nil
	data, _, _, err := p.extractResource(ctx, probe.Text[0], nil, 0)
	if err != nil || !strings.Contains(string(data), "TEXT 030") {
		t.Fatal("negative delay -60 requires the 60..62s cue at position0", err)
	}
	initialText := append([]byte(nil), data...)
	data, _, _, err = p.extractResource(ctx, probe.Text[0], nil, 190)
	if err != nil || !strings.Contains(string(data), "LONG DELAY") {
		t.Fatal("positive delay +60 requires the 20..140s long cue at position190", err)
	}
	for _, track := range probe.Text {
		if _, _, _, err := p.extractResource(ctx, track, nil, 0); err != nil {
			t.Fatalf("extract %s sidecar=%v: %v", track.Format, track.Sidecar != nil, err)
		}
	}
	read := func(p *Playback, name, content string) {
		t.Helper()
		w := httptest.NewRecorder()
		p.Serve(w, httptest.NewRequest("GET", "/"+name, nil), name)
		if w.Code != 200 || !strings.Contains(w.Body.String(), content) {
			t.Fatalf("resource %s: %d %s", name, w.Code, w.Body.String())
		}
	}
	for _, track := range probe.Text {
		read(p, "text-"+track.Ref+"."+track.Format, "000")
	}
	read(p, "font-"+probe.Fonts[0].Ref+".bin", "")
	if p.captions.bytes == 0 || p.Status().Bytes < int64(p.captions.bytes) {
		t.Fatal("resources not budgeted")
	}
	// Decode the actual chosen audio. Metadata alone cannot prove a switch.
	tone := func(p *Playback) float64 {
		t.Helper()
		p.mu.Lock()
		data, _ := p.root.ReadFile("init.mp4")
		for _, seg := range p.segments {
			b, _ := p.root.ReadFile(seg.Name)
			data = append(data, b...)
		}
		p.mu.Unlock()
		temp := filepath.Join(t.TempDir(), "prepared.mp4")
		os.WriteFile(temp, data, 0600)
		pcm, err := service.Runner.runLimit(ctx, bundle.FFmpeg, []string{"-v", "error", "-i", temp, "-map", "0:a:0", "-t", "1", "-ac", "1", "-ar", "48000", "-f", "s16le", "pipe:1"}, service.Runner.resources, 1<<20)
		if err != nil {
			t.Fatal("decode", err)
		}
		cross := 0
		armed := false
		if len(pcm) < 48000*2 {
			t.Fatal("short decoded audio")
		}
		for i := 4800 * 2; i < 43200*2; i += 2 {
			b := int16(uint16(pcm[i]) | uint16(pcm[i+1])<<8)
			if b < -1000 {
				armed = true
			}
			if armed && b > 1000 {
				cross++
				armed = false
			}
		}
		return float64(cross) / 0.8
	}
	one := tone(p)
	directory := p.directory
	p.Close()
	if _, err := os.Stat(directory); !os.IsNotExist(err) {
		t.Fatal("resource cleanup", err)
	}
	// Reproduce continuous progress from0 to40 with an initial0..120 cache.
	// At delay-60 it must refill before the viewer exhausts that cache at60.
	refill := prepare(0, 40)
	t.Cleanup(refill.Close)
	refillName := "text-" + probe.Text[0].Ref + ".vtt"
	refill.mu.Lock()
	refill.captions.cache[refillName] = resource{data: initialText, start: 0, end: 120}
	refill.captions.bytes = len(initialText)
	refill.status.Bytes += int64(len(initialText))
	refill.mu.Unlock()
	response := httptest.NewRecorder()
	refill.Serve(response, httptest.NewRequest("GET", "/"+refillName, nil), refillName)
	refill.Close()
	if response.Code != 200 || !strings.Contains(response.Body.String(), "TEXT 060") || number(response.Header().Get("X-Yane-Text-End")) < 160 {
		t.Fatal("negative-delay cache did not refill early", response.Code)
	}
	p = prepare(1, 156)
	defer p.Close()
	two := tone(p)
	if math.Abs(one-440) > 5 || math.Abs(two-880) > 5 {
		t.Fatalf("audio did not change %.1f -> %.1f", one, two)
	}
	for _, track := range probe.Text {
		read(p, "text-"+track.Ref+"."+track.Format, "078")
	}
	if math.Abs(p.Status().Timeline.AbsoluteAnchor-156) > 4 {
		t.Fatal("unmeasured seek")
	}
	// An Off/seek abort must stop extraction without caching partial cues or
	// stopping the media producer. A new read can then use the same resource lane.
	p.mu.Lock()
	p.captions.cache = map[string]resource{}
	p.status.Bytes -= int64(p.captions.bytes)
	p.captions.bytes = 0
	p.mu.Unlock()
	held := heldRanges{started: make(chan struct{}, 1)}
	service.Backend = held
	requestCtx, abort := context.WithCancel(ctx)
	name := "text-" + probe.Text[0].Ref + ".vtt"
	finished := make(chan struct{})
	go func() {
		defer close(finished)
		p.Serve(httptest.NewRecorder(), httptest.NewRequest("GET", "/"+name, nil).WithContext(requestCtx), name)
	}()
	select {
	case <-held.started:
	case <-time.After(3 * time.Second):
		t.Fatal("extraction did not start")
	}
	abort()
	select {
	case <-finished:
	case <-time.After(3 * time.Second):
		t.Fatal("extraction ignored cancellation")
	}
	if p.captions.bytes != 0 || p.Status().Error != "" {
		t.Fatal("cancel published partial text or failed media")
	}
	service.Backend = siblingRanges{filepath.Dir(path)}
	read(p, name, "078")
	p.Close()
	tail := prepare(1, 190)
	defer tail.Close()
}
