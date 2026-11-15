package media

import (
	"bytes"
	"context"
	"encoding/binary"
	"io"
	"math"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"yanecompanion/internal/engine"
	"yanecompanion/release"
)

type fileRanges struct{ path string }

func (f fileRanges) OpenRange(ctx context.Context, _ string, _ engine.File, start, end int64) (io.ReadCloser, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	file, err := os.Open(f.path)
	if err != nil {
		return nil, err
	}
	return &sectionCloser{io.NewSectionReader(file, start, end-start+1), file}, nil
}

type sectionCloser struct {
	*io.SectionReader
	io.Closer
}

func TestRealContinuousPlayback(t *testing.T) {
	executable := os.Getenv("YANE_TEST_EXECUTABLE")
	if executable == "" {
		t.Skip("requires bundled media executables")
	}
	bundle, err := release.Verify(filepath.Dir(executable))
	if err != nil {
		t.Fatal(err)
	}
	runner := New(bundle.FFmpeg, bundle.FFprobe)
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Second)
	defer cancel()
	path := filepath.Join(t.TempDir(), "a long input.mp4")
	_, err = runner.Prepare(ctx, []string{"-v", "error", "-nostdin", "-stream_loop", "3", "-i", "testdata/tiny-h264.mp4", "-c:v", "copy", "-t", "96", "-movflags", "+faststart", path})
	if err != nil {
		t.Fatal("fixture", err)
	}
	info, _ := os.Stat(path)
	service := &Service{Runner: runner, Backend: fileRanges{path}, Directory: t.TempDir()}
	file := engine.File{Index: 1, Path: "video.mp4", Length: info.Size()}
	probe, err := service.Probe(ctx, "hash", file)
	if err != nil {
		t.Fatal("probe", err)
	}
	if probe.Duration < 95 || !strings.Contains(probe.Video.ContentType, "avc1.") {
		t.Fatalf("actual metadata %+v", probe)
	}
	client := []ClientMedia{{probe.Video.ContentType, true}}
	p, err := service.Prepare(ctx, "hash", file, probe, Target{Position: 0, Output: "original"}, client)
	if err != nil {
		t.Fatal("prepare", err)
	}
	defer p.Close()
	initial := p.Status()
	if !initial.Timeline.Continuous || initial.Timeline.Ended || len(initial.Ranges) != 1 || initial.Timeline.WindowEnd < 6 {
		t.Fatalf("not a measured ready prefix: %+v", initial)
	}
	if err := p.Confirm(math.NaN()); err == nil {
		t.Fatal("NaN progress accepted")
	}
	if err := p.Confirm(90); err == nil {
		t.Fatal("unprepared progress accepted")
	}
	// A paused viewer must stop production even when the entire tiny compressed
	// input fits FFmpeg's input buffer. Reading a segment never confirms progress.
	deadline := time.Now().Add(35 * time.Second)
	for time.Now().Before(deadline) && p.Status().Timeline.WindowEnd < 46 {
		time.Sleep(100 * time.Millisecond)
	}
	paused := p.Status()
	if paused.Timeline.WindowEnd < 46 {
		t.Fatalf("preparation did not fill its window: %+v", paused)
	}
	time.Sleep(1200 * time.Millisecond)
	if latest := p.Status(); latest.Timeline.WindowEnd != paused.Timeline.WindowEnd || latest.Timeline.Ended || latest.Bytes > cacheLimit {
		t.Fatalf("paused preparation kept growing: before=%+v after=%+v", paused, latest)
	}
	if paused.Timeline.WindowEnd > 47 {
		t.Fatalf("time budget bypassed: %+v", paused)
	}
	for p.Status().Timeline.WindowEnd < 94 {
		status := p.Status()
		if status.Error != "" {
			t.Fatal(status.Error)
		}
		if err = p.Confirm(status.Timeline.WindowEnd - 4); err != nil {
			t.Fatal(err)
		}
		select {
		case <-ctx.Done():
			t.Fatal("continuous output stalled")
		case <-time.After(100 * time.Millisecond):
		}
	}
	if err = p.Confirm(93); err != nil {
		t.Fatal(err)
	}
	select {
	case <-p.done:
	case <-ctx.Done():
		t.Fatal("EOF deadline")
	}
	final := p.Status()
	if final.Error != "" || !final.Timeline.Ended || final.Timeline.WindowEnd < 95 || final.Ranges[0].Start <= 0 {
		t.Fatalf("continuous EOF/eviction failed: %+v", final)
	}
	w := httptest.NewRecorder()
	p.Serve(w, httptest.NewRequest("GET", "http://local/media.m3u8", nil), "media.m3u8")
	if w.Code != 200 || !strings.Contains(w.Body.String(), "#EXT-X-ENDLIST") || strings.Contains(w.Body.String(), "segment-000000.m4s") {
		t.Fatal("wrong final manifest", w.Body.String())
	}
	// A new preparation seeks to the measured prior GOP, without recycling URLs.
	p.Close()
	seek, err := service.Prepare(ctx, "hash", file, probe, Target{Position: 19.3, Output: "original"}, client)
	if err != nil {
		t.Fatal("seek", err)
	}
	seekStatus := seek.Status()
	if seekStatus.Timeline.AbsoluteAnchor > 19.3 || seekStatus.Timeline.AbsoluteAnchor < 17 || seekStatus.Timeline.WindowEnd <= 19.3 {
		t.Fatalf("seek anchor %+v", seekStatus)
	}
	directory := seek.directory
	seek.Close()
	if _, err := os.Stat(directory); !os.IsNotExist(err) {
		t.Fatal("cancel did not remove generated files", err)
	}
}

func TestRealPlaybackFinalSeconds(t *testing.T) {
	executable := os.Getenv("YANE_TEST_EXECUTABLE")
	if executable == "" {
		t.Skip("requires bundled media executables")
	}
	bundle, err := release.Verify(filepath.Dir(executable))
	if err != nil {
		t.Fatal(err)
	}
	path := "testdata/tiny-h264.mp4"
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	service := &Service{Runner: New(bundle.FFmpeg, bundle.FFprobe), Backend: fileRanges{path}, Directory: t.TempDir()}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	file := engine.File{Index: 1, Path: "video.mp4", Length: info.Size()}
	probe, err := service.Probe(ctx, "hash", file)
	if err != nil {
		t.Fatal(err)
	}
	for _, remaining := range []float64{4, .2, 4, .2} {
		position := probe.Duration - remaining
		p, err := service.Prepare(ctx, "hash", file, probe, Target{Position: position, Output: "original"}, []ClientMedia{{probe.Video.ContentType, true}})
		if err != nil {
			t.Fatalf("remaining %.1fs: %v", remaining, err)
		}
		status := p.Status()
		if status.Error != "" || len(status.Ranges) == 0 || status.Timeline.WindowEnd < position {
			p.Close()
			t.Fatalf("remaining %.1fs not readable: %+v", remaining, status)
		}
		p.Close()
	}
}

func TestMP4RejectsMalformedLengthsAndMissingClocks(t *testing.T) {
	for _, data := range [][]byte{{}, {0, 0, 0, 7, 'm', 'o', 'o', 'v'}, {0, 0, 0, 20, 'm', 'o', 'o', 'v'}, {0, 0, 0, 1, 'm', 'o', 'o', 'v'}} {
		if _, err := initClock(data); err == nil {
			t.Fatalf("invalid init accepted %x", data)
		}
	}
	if _, _, err := readBox(bytes.NewReader([]byte{0xff, 0xff, 0xff, 0xff, 'm', 'd', 'a', 't'})); err == nil {
		t.Fatal("unbounded allocation")
	}
	if _, err := (mp4Clock{video: 1}).fragment([]byte{0, 0, 0, 8, 'm', 'o', 'o', 'f'}); err == nil {
		t.Fatal("missing timestamp accepted")
	}
	if !math.IsNaN(frameAnchor([]byte("#tb 0: 1/0\n0, 0, 0, 1"))) {
		t.Fatal("invalid timebase")
	}
}

func TestOriginalPolicyRetains4KHDRAndNegotiatesAudio(t *testing.T) {
	for _, video := range []Video{{Codec: "h264", ContentType: `video/mp4; codecs="avc1.640033"`, Width: 3840, Height: 2160}, {Codec: "hevc", ContentType: `video/mp4; codecs="hvc1.2.4.L153.B0"`, Width: 3840, Height: 2160, BitDepth: 10, DynamicRange: "hdr"}} {
		p := &Probe{Duration: 100, Video: video, Audio: []Audio{{Ref: "audio", Codec: "dts", Channels: 6}}}
		target := Target{AudioRef: "audio", Output: "original", Position: 50}
		_, copyAudio, err := p.Validate(target, []ClientMedia{{video.ContentType, true}, {`audio/mp4; codecs="mp4a.40.2"`, true}})
		if err != nil || copyAudio {
			t.Fatal("independent AAC conversion was lost", err)
		}
		if _, _, err = p.Validate(target, []ClientMedia{{video.ContentType, false}}); err == nil {
			t.Fatal("unsupported browser video allowed")
		}
		target.Output = "sdr-1080p"
		if _, _, err = p.Validate(target, nil); err == nil {
			t.Fatal("undeclared video encoder allowed")
		}
	}
}

type pausedResponse struct {
	headers          http.Header
	entered, release chan struct{}
}

func (w *pausedResponse) Header() http.Header { return w.headers }
func (w *pausedResponse) WriteHeader(int)     {}
func (w *pausedResponse) Write(data []byte) (int, error) {
	close(w.entered)
	<-w.release
	return len(data), nil
}

func TestProgressRetainsSegmentUntilOpenResponseCloses(t *testing.T) {
	directory := t.TempDir()
	root, err := os.OpenRoot(directory)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	if err = root.WriteFile("segment-000000.m4s", []byte("data"), 0600); err != nil {
		t.Fatal(err)
	}
	if err = root.WriteFile("segment-000001.m4s", []byte("next"), 0600); err != nil {
		t.Fatal(err)
	}
	p := &Playback{ctx: context.Background(), root: root, readers: make(chan struct{}, 8), openFiles: map[string]int{}, status: PlaybackStatus{Bytes: 8, Timeline: SourceTimeline{Duration: 20, WindowEnd: 20}}, segments: []segment{{Name: "segment-000000.m4s", Start: 0, End: 2}, {Name: "segment-000001.m4s", Start: 2, End: 20}}}
	w := &pausedResponse{headers: http.Header{}, entered: make(chan struct{}), release: make(chan struct{})}
	done := make(chan struct{})
	go func() {
		p.Serve(w, httptest.NewRequest("GET", "http://local/segment", nil), "segment-000000.m4s")
		close(done)
	}()
	<-w.entered
	if err = p.Confirm(12); err != nil {
		close(w.release)
		<-done
		t.Fatal("active response broke eviction", err)
	}
	if _, err = root.Stat("segment-000000.m4s"); err != nil {
		close(w.release)
		<-done
		t.Fatal("open response was evicted")
	}
	close(w.release)
	<-done
	if err = p.Confirm(12); err != nil {
		t.Fatal(err)
	}
	if _, err = root.Stat("segment-000000.m4s"); !os.IsNotExist(err) {
		t.Fatal("closed consumed segment was retained", err)
	}
}

type zeroReader struct{}

func (zeroReader) Read(data []byte) (int, error) { clear(data); return len(data), nil }

func highBitrateInput(t *testing.T) io.Reader {
	t.Helper()
	raw, err := os.ReadFile("testdata/tiny-h264.mp4")
	if err != nil {
		t.Fatal(err)
	}
	reader := bytes.NewReader(raw)
	var parts []io.Reader
	for count := 0; count < 2; {
		data, kind, err := readBox(reader)
		if err != nil {
			t.Fatal(err)
		}
		switch kind {
		case "ftyp", "moov", "moof":
			parts = append(parts, bytes.NewReader(data))
		case "mdat":
			const size = 25 << 20
			header := append([]byte(nil), data[:8]...)
			binary.BigEndian.PutUint32(header, size)
			parts = append(parts, bytes.NewReader(header), bytes.NewReader(data[8:]), io.LimitReader(zeroReader{}, int64(size-len(data))))
			count++
		}
	}
	return io.MultiReader(parts...)
}

func TestHighBitratePrefixStartsBeforeByteBackpressure(t *testing.T) {
	root, err := os.OpenRoot(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	p := &Playback{ctx: ctx, ready: make(chan struct{}), root: root, input: &inputBridge{}, window: 8, status: PlaybackStatus{Timeline: SourceTimeline{Duration: 24}}, openFiles: map[string]int{}}
	done := make(chan error, 1)
	input := highBitrateInput(t)
	go func() { done <- p.consume(ctx, input) }()
	select {
	case <-p.ready:
	case <-time.After(5 * time.Second):
		cancel()
		<-done
		t.Fatal("valid prefix could not start under byte pressure")
	}
	status := p.Status()
	if status.Timeline.WindowEnd != 4 || status.Bytes < cacheTarget || status.Bytes > cacheLimit {
		cancel()
		<-done
		t.Fatalf("invalid bounded startup %+v", status)
	}
	cancel()
	<-done
}

func TestBudgetBeforeRequestedSeekFailsWithoutPublishingReady(t *testing.T) {
	root, err := os.OpenRoot(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	p := &Playback{ctx: ctx, ready: make(chan struct{}), root: root, input: &inputBridge{}, position: 20, window: 8, status: PlaybackStatus{Timeline: SourceTimeline{Duration: 24}}, openFiles: map[string]int{}}
	err = p.consume(ctx, highBitrateInput(t))
	if err == nil || err.Error() != "RESOURCE_LIMIT" || p.Status().Error != "RESOURCE_LIMIT" {
		t.Fatalf("budget hid unavailable target: %v %+v", err, p.Status())
	}
	select {
	case <-p.ready:
		t.Fatal("unavailable requested position became ready")
	default:
	}
}

func TestRequestedSeekDoesNotEvictInitialDecoderChain(t *testing.T) {
	root, err := os.OpenRoot(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	if err = root.WriteFile("segment-000000.m4s", []byte("keyframe"), 0600); err != nil {
		t.Fatal(err)
	}
	p := &Playback{root: root, position: 20, status: PlaybackStatus{Bytes: 16, Timeline: SourceTimeline{Duration: 30, WindowEnd: 24}}, segments: []segment{{Name: "segment-000000.m4s", Start: 0, End: 2}, {Name: "segment-000001.m4s", Start: 2, End: 24}}}
	if err = p.evict(); err != nil {
		t.Fatal(err)
	}
	if _, err = root.Stat("segment-000000.m4s"); err != nil {
		t.Fatal("requested seek discarded unfetched decoder chain")
	}
	if err = p.Confirm(20); err != nil {
		t.Fatal(err)
	}
	if _, err = root.Stat("segment-000000.m4s"); !os.IsNotExist(err) {
		t.Fatal("confirmed consumption retained old chain", err)
	}
}
