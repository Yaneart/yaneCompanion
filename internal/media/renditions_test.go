package media

import (
	"context"
	"encoding/binary"
	"io"
	"math"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"yanecompanion/internal/engine"
	"yanecompanion/release"
)

type renditionRanges struct {
	fileRanges
	block   atomic.Bool
	started chan struct{}
}

func TestRenditionResourceCacheSharesVideoBudget(t *testing.T) {
	probe := &Probe{}
	probe.captionCache.hash = "file"
	probe.captionCache.resources = map[string]resource{"text": {data: []byte("ab")}}
	p := &Playback{ctx: context.Background(), byteLimit: 112 << 20, status: PlaybackStatus{Bytes: (112 << 20) - 1}, captions: &captions{hash: "file", probe: probe}}
	p.restoreCaptionCache()
	if p.captions.bytes != 0 {
		t.Fatal("caption cache exceeded the video's share of the aggregate limit")
	}
	p.byteLimit = 0
	p.restoreCaptionCache()
	if p.captions.bytes != 2 {
		t.Fatal("legacy muxed cache limit changed")
	}
}

func (f *renditionRanges) OpenRange(ctx context.Context, hash string, file engine.File, start, end int64) (io.ReadCloser, error) {
	if f.block.Load() {
		select {
		case f.started <- struct{}{}:
		default:
		}
		<-ctx.Done()
		return nil, ctx.Err()
	}
	return f.fileRanges.OpenRange(ctx, hash, file, start, end)
}

func TestRealRenditionProducers(t *testing.T) {
	exe := os.Getenv("YANE_TEST_EXECUTABLE")
	if exe == "" {
		t.Skip("requires bundled media")
	}
	bundle, err := release.Verify(filepath.Dir(exe))
	if err != nil {
		t.Fatal(err)
	}
	runner := New(bundle.FFmpeg, bundle.FFprobe)
	deadline := 60 * time.Second
	if os.Getenv("YANE_RENDITION_BROWSER") != "" {
		deadline = 5 * time.Minute
	}
	ctx, cancel := context.WithTimeout(context.Background(), deadline)
	defer cancel()
	first := waveFixture(t)
	data, err := os.ReadFile(first)
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < (len(data)-44)/2; i++ {
		binary.LittleEndian.PutUint16(data[44+i*2:], uint16(int16(math.Sin(float64(i)*2*math.Pi*880/48000)*16000)))
	}
	second := filepath.Join(t.TempDir(), "second.wav")
	if err = os.WriteFile(second, data, 0600); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "renditions.mkv")
	_, err = runner.Prepare(ctx, []string{"-v", "error", "-nostdin", "-i", "testdata/tiny-h264.mp4", "-stream_loop", "-1", "-i", first, "-stream_loop", "-1", "-i", second, "-map", "0:v:0", "-map", "1:a:0", "-map", "2:a:0", "-c:v", "copy", "-c:a", "pcm_s16le", "-filter:a:0", "atrim=duration=24", "-t", "28", path})
	if err != nil {
		t.Fatal("fixture", err)
	}
	info, _ := os.Stat(path)
	backend := &renditionRanges{fileRanges: fileRanges{path}, started: make(chan struct{}, 1)}
	service := &Service{Runner: runner, Backend: backend, Directory: t.TempDir()}
	file := engine.File{Index: 1, Path: "renditions.mkv", Length: info.Size()}
	probe, err := service.Probe(ctx, "hash", file)
	if err != nil {
		t.Fatal("probe", err)
	}
	client := []ClientMedia{{probe.Video.ContentType, true}, {`audio/mp4; codecs="mp4a.40.2"`, true}}
	r, err := service.PrepareRenditions(ctx, "hash", file, probe, Target{AudioRef: probe.Audio[0].Ref, Position: 12, Output: "original"}, client)
	if err != nil {
		t.Fatal("prepare", err)
	}
	defer r.Close()
	if directory := os.Getenv("YANE_RENDITION_BROWSER"); directory != "" {
		serveRenditionBrowser(t, ctx, r, directory)
		return
	}
	video, directory := r.video, r.video.directory
	initial := r.Status()
	if initial.Error != "" || initial.Timeline.AbsoluteAnchor > 12.1 || initial.Timeline.AbsoluteAnchor < 8 || math.Abs(initial.Timeline.Duration-24) > .1 {
		t.Fatalf("unmeasured initial source: %+v", initial)
	}
	if err = r.Confirm(12); err != nil {
		t.Fatal("confirm", err)
	}
	old := r.audio
	changed, err := r.SelectAudio(ctx, probe.Audio[1].Ref, 14)
	if err != nil {
		t.Fatal("switch", err)
	}
	if r.video != video || r.video.directory != directory || r.audio == old || changed.Duration < 27.9 {
		t.Fatal("audio replaced video or lost selected duration")
	}
	if r.retired != old {
		t.Fatal("in-flight reads lost the previous audio generation")
	}
	select {
	case <-old.done:
	default:
		t.Fatal("previous audio worker remains active")
	}
	if math.Abs(r.audio.Status().Timeline.AbsoluteAnchor-r.video.Status().Timeline.AbsoluteAnchor) > 3 {
		t.Fatal("independent producer lost the file clock")
	}
	playlist := httptest.NewRecorder()
	r.Serve(playlist, httptest.NewRequest("GET", "/", nil), "audio-"+probe.Audio[1].Ref+".m3u8")
	_, clock, found := strings.Cut(playlist.Body.String(), "#EXT-X-PROGRAM-DATE-TIME:")
	clock, _, _ = strings.Cut(clock, "\n")
	date, dateErr := time.Parse(time.RFC3339Nano, clock)
	origin := time.Date(2000, 1, 1, 0, 0, 0, 0, time.UTC)
	if playlist.Code != 200 || !found || dateErr != nil || date.Before(origin.Add(8*time.Second)) || date.After(origin.Add(14*time.Second)) {
		t.Fatal("lazy audio lost its source-scoped playlist clock", playlist.Code)
	}
	// Same selection is an idempotent read; regeneration must rotate resource IDs.
	active := r.audio
	again, err := r.SelectAudio(ctx, probe.Audio[1].Ref, 14)
	if err != nil || again != changed || active != r.audio {
		t.Fatal("selection replay restarted audio", err)
	}
	cancelled, abort := context.WithCancel(ctx)
	abort()
	if _, err = r.SelectAudio(cancelled, probe.Audio[0].Ref, 14); err == nil {
		t.Fatal("cancelled selection admitted")
	}
	if _, err = r.SelectAudio(ctx, "unknown", 14); err == nil {
		t.Fatal("unprobed track admitted")
	}
	if r.audio != active {
		t.Fatal("invalid selection destroyed active audio")
	}
	back, err := r.SelectAudio(ctx, probe.Audio[0].Ref, 14)
	if err != nil || back.Generation == changed.Generation {
		t.Fatal("audio generation reused", err)
	}
	if video.cacheLimit()+r.audio.cacheLimit()+r.retired.cacheLimit() != cacheLimit {
		t.Fatal("aggregate cache no longer bounded")
	}
	backend.block.Store(true)
	interrupted, abortSelection := context.WithCancel(ctx)
	result := make(chan error, 1)
	go func() { _, err := r.SelectAudio(interrupted, probe.Audio[1].Ref, 14); result <- err }()
	select {
	case <-backend.started:
	case <-ctx.Done():
		t.Fatal("audio input did not start")
	}
	abortSelection()
	select {
	case err := <-result:
		if err == nil {
			t.Fatal("cancelled producer published")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("cancel did not join audio worker")
	}
	if r.video != video || r.audioRef != probe.Audio[0].Ref || r.audio.Status().Error != "" {
		t.Fatal("cancellation changed active audio/video")
	}
	backend.block.Store(false)
	if _, err = r.SelectAudio(ctx, probe.Audio[0].Ref, 14); err != nil {
		t.Fatal("retry after cancel", err)
	}
	entries, err := os.ReadDir(service.Directory)
	if err != nil || len(entries) != 2 {
		t.Fatal("cancelled audio artifacts remain", len(entries), err)
	}
	for !r.Status().Timeline.Ended {
		s := r.Status()
		if s.Error != "" {
			t.Fatal("EOF", s.Error)
		}
		if s.Bytes > cacheLimit {
			t.Fatal("aggregate cache overflow")
		}
		position := math.Min(s.Timeline.WindowEnd-.1, 23.9)
		if err = r.Confirm(position); err != nil {
			t.Fatal("progress", err)
		}
		select {
		case <-ctx.Done():
			t.Fatal("EOF deadline")
		case <-time.After(50 * time.Millisecond):
		}
	}
	clamped, err := r.SelectAudio(ctx, probe.Audio[0].Ref, 28)
	if err != nil || clamped.Position == nil || math.Abs(*clamped.Position-23.99) > .01 || r.video != video {
		t.Fatal("shorter selection did not retain video and correct its position", err)
	}
	r.Close()
	if _, err = os.Stat(directory); !os.IsNotExist(err) {
		t.Fatal("video artifacts remain", err)
	}
	if _, err = r.SelectAudio(ctx, probe.Audio[0].Ref, 14); err == nil {
		t.Fatal("closed source admitted selection")
	}
}
