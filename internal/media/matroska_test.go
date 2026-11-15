package media

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"yanecompanion/internal/engine"
	"yanecompanion/release"
	"yanecompanion/test/fixture"
)

type indexedRanges struct {
	mu           sync.Mutex
	data         []byte
	bytes, calls int
	err          error
}

type failingIndexRanges struct {
	mu     sync.Mutex
	source *indexedRanges
	reads  int
	err    error
}

func (f *failingIndexRanges) OpenRange(ctx context.Context, hash string, file engine.File, start, end int64) (io.ReadCloser, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.reads == 0 {
		return nil, f.err
	}
	f.reads--
	return f.source.OpenRange(ctx, hash, file, start, end)
}

func (b *indexedRanges) OpenRange(ctx context.Context, _ string, _ engine.File, start, end int64) (io.ReadCloser, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if b.err != nil {
		return nil, b.err
	}
	if start < 0 || end < start || end >= int64(len(b.data)) {
		return nil, errors.New("invalid range")
	}
	b.bytes += int(end - start + 1)
	b.calls++
	return io.NopCloser(bytes.NewReader(b.data[start : end+1])), nil
}

func (b *indexedRanges) reader(ctx context.Context) *matroskaReader {
	return &matroskaReader{ctx: ctx, backend: b, file: engine.File{Path: "fixture.mkv", Length: int64(len(b.data))}}
}

type indexFixtureOptions struct {
	simple, noRelative, noDuration, badOffset, codecState, wrongTrack, wrongTime, wrongDuration, laced bool
	shift                                                                                              int64
	ass                                                                                                bool
}

func indexFixture(o indexFixtureOptions) []byte {
	field := func(id, value int64) []byte {
		var b [4]byte
		binary.BigEndian.PutUint32(b[:], uint32(value))
		return ebmlWrap(id, b[:])
	}
	join := func(parts ...[]byte) []byte { return bytes.Join(parts, nil) }
	header := ebmlWrap(0x1a45dfa3, join(field(0x4286, 1), field(0x42f7, 1), field(0x42f2, 4), field(0x42f3, 8), ebmlWrap(0x4282, []byte("matroska")), field(0x4287, 4), field(0x4285, 2)))
	info := ebmlWrap(0x1549a966, field(0x2ad7b1, 1000000))
	codec, private, text := "S_TEXT/UTF8", []byte(nil), []byte("Indexed cue")
	if o.ass {
		codec = "S_TEXT/ASS"
		private = ebmlWrap(0x63a2, []byte("[Script Info]\nScriptType: v4.00+\n[V4+ Styles]\nFormat: Name, Fontname, Fontsize, PrimaryColour, SecondaryColour, OutlineColour, BackColour, Bold, Italic, Underline, StrikeOut, ScaleX, ScaleY, Spacing, Angle, BorderStyle, Outline, Shadow, Alignment, MarginL, MarginR, MarginV, Encoding\nStyle: Default,Arial,18,&H00FFFFFF,&H000000FF,&H00000000,&H80000000,0,0,0,0,100,100,0,0,1,1,0,2,10,10,12,1\n[Events]\nFormat: Layer, Start, End, Style, Name, MarginL, MarginR, MarginV, Effect, Text\n"))
		text = []byte("0,0,Default,,0,0,0,,Indexed cue")
	}
	tracks := ebmlWrap(0x1654ae6b, ebmlWrap(0xae, join(field(0xd7, 7), field(0x73c5, 7), field(0x83, 17), ebmlWrap(0x86, []byte(codec)), private)))
	timestamp := field(0xe7, 1000+o.shift)
	packet := join([]byte{0x87, 0, 5, 0x80}, text)
	if o.wrongTrack {
		packet[0] = 0x88
	}
	if o.wrongTime {
		packet[2]++
	}
	if o.laced {
		packet[3] |= 0x02
	}
	duration := int64(2000)
	if o.wrongDuration {
		duration++
	}
	block := ebmlWrap(0xa0, join(ebmlWrap(0xa1, packet), field(0x9b, duration)))
	if o.simple {
		block = ebmlWrap(0xa3, packet)
	}
	cluster := ebmlWrap(0x1f43b675, join(timestamp, block, ebmlWrap(0xec, make([]byte, 1<<20))))
	cues := func(position int64) []byte {
		fields := join(field(0xf7, 7), field(0xf1, position))
		if !o.noRelative {
			relative := int64(len(timestamp))
			if o.badOffset {
				relative = 0xffffff
			}
			fields = append(fields, field(0xf0, relative)...)
		}
		if !o.noDuration {
			fields = append(fields, field(0xb2, 2000)...)
		}
		if o.codecState {
			fields = append(fields, field(0xea, 1)...)
		}
		return ebmlWrap(0x1c53bb6b, ebmlWrap(0xbb, join(field(0xb3, 1005+o.shift), ebmlWrap(0xb7, fields))))
	}
	index := cues(int64(len(info) + len(tracks) + len(cues(0))))
	return join(header, ebmlWrap(0x18538067, join(info, tracks, index, cluster)))
}

func TestMatroskaIndexedRanges(t *testing.T) {
	for _, o := range []indexFixtureOptions{{}, {simple: true}, {noRelative: true}, {noDuration: true}, {noRelative: true, noDuration: true}} {
		b := &indexedRanges{data: indexFixture(o)}
		r := b.reader(context.Background())
		index, err := loadMatroskaIndex(r)
		if err != nil {
			t.Fatal(err)
		}
		input, err := index.window(r, 0, "vtt", 1.1, 1.5)
		limit := 2500
		if o.noRelative || o.noDuration {
			limit = 17000
		}
		if err != nil || !bytes.Contains(input, []byte("Indexed cue")) || len(input) > 1500 || b.bytes > limit {
			t.Fatalf("options=%+v: input=%d read=%d err=%v", o, len(input), b.bytes, err)
		}
		calls := b.calls
		input, err = index.window(r, 0, "vtt", 5, 8)
		if err != nil || len(input) != 0 || b.calls != calls {
			t.Fatal("empty window read payload", err)
		}
	}
}

func TestMatroskaUnsafeIndex(t *testing.T) {
	for name, o := range map[string]indexFixtureOptions{
		"simple missing duration": {simple: true, noDuration: true}, "state": {codecState: true}, "offset": {badOffset: true},
		"track": {wrongTrack: true}, "timestamp": {wrongTime: true}, "block duration": {wrongDuration: true}, "lacing": {laced: true},
		"scan wrong timestamp": {noRelative: true, noDuration: true, wrongTime: true}, "scan wrong track": {noRelative: true, noDuration: true, wrongTrack: true},
		"scan wrong duration": {noRelative: true, wrongDuration: true}, "scan wrong offset": {noDuration: true, badOffset: true},
	} {
		t.Run(name, func(t *testing.T) {
			r := (&indexedRanges{data: indexFixture(o)}).reader(context.Background())
			index, err := loadMatroskaIndex(r)
			if err == nil {
				_, err = index.window(r, 0, "vtt", 1, 2)
			}
			if !errors.Is(err, errMatroskaIndex) {
				t.Fatal("unsafe index accepted", err)
			}
		})
	}
	r := (&indexedRanges{data: indexFixture(indexFixtureOptions{})}).reader(context.Background())
	index, err := loadMatroskaIndex(r)
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		stream     int
		format     string
		start, end float64
	}{{1, "vtt", 1, 2}, {0, "ass", 1, 2}, {0, "vtt", math.NaN(), 2}, {0, "vtt", 2, 1}} {
		if _, err := index.window(r, tc.stream, tc.format, tc.start, tc.end); !errors.Is(err, errMatroskaIndex) {
			t.Fatal("invalid selection accepted", err)
		}
	}
	for _, data := range [][]byte{make([]byte, 16), {0x1a, 0x45, 0xdf, 0xa3, 0xff, 0xff}, {0, 0, 0}} {
		if _, err := loadMatroskaIndex((&indexedRanges{data: data}).reader(context.Background())); !errors.Is(err, errMatroskaIndex) {
			t.Fatal("malformed EBML", err)
		}
	}
}

func TestMatroskaReadContracts(t *testing.T) {
	b := &indexedRanges{data: indexFixture(indexFixtureOptions{})}
	r := b.reader(context.Background())
	for _, tc := range []struct{ offset, length int64 }{{-1, 1}, {0, 0}, {0, 2<<20 + 1}, {int64(len(b.data)), 1}} {
		if _, err := r.read(tc.offset, tc.length); !errors.Is(err, errMatroskaIndex) {
			t.Fatal("unsafe read", err)
		}
	}
	r.bytes = 8 << 20
	if _, err := r.read(0, 1); !errors.Is(err, errMatroskaIndex) {
		t.Fatal("byte budget", err)
	}
	r.bytes, r.requests = 0, 512
	if _, err := r.read(0, 1); !errors.Is(err, errMatroskaIndex) {
		t.Fatal("request budget", err)
	}
	if b.calls != 0 {
		t.Fatal("limited read reached backend")
	}
	scanned := b.reader(context.Background())
	scanned.scan = true
	if _, err := scanned.read(int64(len(b.data))-1, 2); !errors.Is(err, errMatroskaIndex) {
		t.Fatal("read-ahead accepted truncated range", err)
	}
	failure := errors.New("upstream failed")
	b.err = failure
	if _, err := loadMatroskaIndex(b.reader(context.Background())); !errors.Is(err, failure) {
		t.Fatal("masked upstream failure", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := loadMatroskaIndex(b.reader(ctx)); !errors.Is(err, context.Canceled) {
		t.Fatal("masked cancellation", err)
	}
}

func TestRealIndexedSubtitleDecode(t *testing.T) {
	exe := os.Getenv("YANE_TEST_EXECUTABLE")
	if exe == "" {
		t.Skip("requires bundled tools")
	}
	bundle, err := release.Verify(filepath.Dir(exe))
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	for _, ass := range []bool{false, true} {
		for _, shift := range []int64{0, 3600000} {
			b := &indexedRanges{data: indexFixture(indexFixtureOptions{ass: ass, shift: shift, noRelative: true, noDuration: true})}
			format := "vtt"
			if ass {
				format = "ass"
			}
			probe := &Probe{Duration: 10, sourceStart: float64(shift) / 1000}
			p := &Playback{ctx: ctx}
			p.captions = &captions{service: &Service{Runner: New(bundle.FFmpeg, bundle.FFprobe), Backend: b}, file: engine.File{Length: int64(len(b.data))}, probe: probe}
			data, ok, err := p.indexedText(ctx, Text{Index: 0, Format: format}, 0, 5, nil)
			if err != nil || !ok || !strings.Contains(string(data), "Indexed cue") || !strings.Contains(string(data), "00:01.") {
				t.Fatalf("ass=%v shift=%d: ok=%v err=%v output=%s", ass, shift, ok, err, data)
			}
			if b.bytes > 18000 {
				t.Fatal("indexed decoder read media payload", b.bytes)
			}
			loadedBytes := b.bytes
			replacement := &Playback{ctx: ctx, captions: &captions{service: p.captions.service, file: p.captions.file, probe: probe}}
			if _, ok, err := replacement.indexedText(ctx, Text{Index: 0, Format: format}, 0, 5, nil); err != nil || !ok || replacement.captions.index != p.captions.index || b.bytes-loadedBytes >= loadedBytes {
				t.Fatal("replacement did not reuse the scoped index", ok, err)
			}
		}
	}
	// The normal FFmpeg muxer must also produce a usable index, including ASS
	// codec private data and a long cue overlapping the requested window.
	path := fixture.Tracks(t, bundle.FFmpeg, "testdata/tiny-h264.mp4")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	b := &indexedRanges{data: data}
	service := &Service{Runner: New(bundle.FFmpeg, bundle.FFprobe), Backend: b}
	file := engine.File{Length: int64(len(data))}
	probe, err := service.Probe(ctx, "hash", file)
	if err != nil {
		t.Fatal(err)
	}
	p := &Playback{ctx: ctx, captions: &captions{service: service, file: file, probe: probe}}
	b.bytes, b.calls = 0, 0
	for _, track := range probe.Text {
		output, ok, err := p.indexedText(ctx, track, 130, 160, nil)
		if err != nil || !ok || !strings.Contains(string(output), "078") {
			t.Fatalf("real %s indexed=%v: %v %s", track.Format, ok, err, output)
		}
		if track.Format == "vtt" && !strings.Contains(string(output), "LONG DELAY") {
			t.Fatal("lost overlapping cue")
		}
	}
	t.Logf("real indexed tracks: %d bytes / %d ranges", b.bytes, b.calls)
	if b.bytes > 128<<10 {
		t.Fatal("subtitle decode read media", b.bytes)
	}
}

func TestCaptionAdmissionAfterCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	track := Text{Ref: "track", Format: "vtt", Available: true}
	name := "text-track.vtt"
	p := &Playback{ctx: ctx, captions: &captions{probe: &Probe{Duration: 100, Text: []Text{track}}, busy: make(chan struct{}, 1), cache: map[string]resource{name: {data: []byte("WEBVTT\n\n"), start: 0, end: 100}}}}
	p.captions.busy <- struct{}{}
	w := httptest.NewRecorder()
	done := make(chan struct{})
	go func() { defer close(done); p.serveResource(w, httptest.NewRequest("GET", "/"+name, nil), name) }()
	// A cancelled predecessor can finish shortly after the new browser request.
	time.Sleep(20 * time.Millisecond)
	<-p.captions.busy
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("resource lane did not recover")
	}
	if w.Code != 200 || w.Body.String() != "WEBVTT\n\n" {
		t.Fatal("rapid switch rejected", w.Code)
	}
	p.captions.busy <- struct{}{}
	requestCtx, abort := context.WithCancel(ctx)
	abort()
	p.serveResource(httptest.NewRecorder(), httptest.NewRequest("GET", "/"+name, nil).WithContext(requestCtx), name)
	if len(p.captions.busy) != 1 {
		t.Fatal("cancelled waiter consumed resource lane")
	}
	<-p.captions.busy
}

func TestCaptionDeadlineReturnsHTTPError(t *testing.T) {
	p := &Playback{ctx: context.Background(), captions: &captions{service: &Service{Backend: heldRanges{started: make(chan struct{}, 1)}}, file: engine.File{Length: 100}, probe: &Probe{Duration: 100, Text: []Text{{Ref: "track", Format: "vtt", Available: true}}}, busy: make(chan struct{}, 1), cache: map[string]resource{}}}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx, cancel := context.WithTimeout(r.Context(), 20*time.Millisecond)
		defer cancel()
		p.serveResource(w, r.WithContext(ctx), "text-track.vtt")
	}))
	defer server.Close()
	client := &http.Client{Timeout: 2 * time.Second}
	response, err := client.Get(server.URL)
	if err != nil {
		t.Fatal("processing deadline dropped HTTP error", err)
	}
	defer response.Body.Close()
	if response.StatusCode != 422 {
		t.Fatal("deadline response", response.StatusCode)
	}
	if p.captions.indexChecked || len(p.captions.cache) != 0 {
		t.Fatal("timeout published resources")
	}
}

func TestCaptionCacheReplacementScopeAndBudget(t *testing.T) {
	ctx := context.Background()
	track := Text{Ref: "track", Format: "vtt", Available: true}
	file := engine.File{Index: 1, Path: "episode.mkv", Length: 1000}
	probe := &Probe{Duration: 100, Text: []Text{track}}
	shared := &probe.captionCache
	shared.hash, shared.file = "hash", file
	shared.resources = map[string]resource{"text-track.vtt": {data: []byte("WEBVTT\n\n"), start: 0, end: 100}}
	newPlayback := func(hash string, selected engine.File) *Playback {
		p := &Playback{ctx: ctx, captions: &captions{hash: hash, file: selected, probe: probe, busy: make(chan struct{}, 1), cache: map[string]resource{}}}
		p.restoreCaptionCache()
		return p
	}
	p := newPlayback("hash", file)
	w := httptest.NewRecorder()
	p.serveResource(w, httptest.NewRequest("GET", "/text-track.vtt", nil), "text-track.vtt")
	if w.Code != 200 || p.captions.bytes != len("WEBVTT\n\n") || p.status.Bytes != int64(p.captions.bytes) {
		t.Fatal("cached text not restored/accounted", w.Code)
	}
	delete(p.captions.cache, "text-track.vtt")
	if len(shared.resources) != 1 {
		t.Fatal("replacement mutated shared cache")
	}
	otherFile := file
	otherFile.Index++
	for _, p := range []*Playback{newPlayback("other", file), newPlayback("hash", otherFile)} {
		if len(p.captions.cache) != 0 || p.captions.bytes != 0 {
			t.Fatal("caption scope crossed")
		}
	}
	shared.resources["too-large"] = resource{data: make([]byte, resourceLimit)}
	if len(newPlayback("hash", file).captions.cache) != 0 {
		t.Fatal("restored cache exceeded limit")
	}
}

func TestMatroskaCancelledIndexIsRetriable(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	held := heldRanges{started: make(chan struct{}, 1)}
	probe := &Probe{Duration: 10}
	service := &Service{Backend: held}
	p := &Playback{ctx: ctx, captions: &captions{service: service, probe: probe, file: engine.File{Length: int64(len(indexFixture(indexFixtureOptions{})))}}}
	job, abort := context.WithCancel(ctx)
	done := make(chan error, 1)
	go func() { _, _, err := p.indexedText(job, Text{Index: 0, Format: "vtt"}, 0, 5, nil); done <- err }()
	<-held.started
	abort()
	if err := <-done; !errors.Is(err, context.Canceled) {
		t.Fatal("cancellation lost", err)
	}
	if p.captions.indexChecked || probe.captionCache.checked || p.captions.bytes != 0 {
		t.Fatal("partial index was cached")
	}
	service.Backend = &indexedRanges{data: indexFixture(indexFixtureOptions{})}
	// An empty VTT window requires no worker and must retry the failed index.
	data, ok, err := p.indexedText(ctx, Text{Index: 0, Format: "vtt"}, 5, 8, nil)
	if err != nil || !ok || !bytes.HasPrefix(data, []byte("WEBVTT\n\n")) || !probe.captionCache.checked {
		t.Fatal("index retry", ok, err)
	}
}

func sparseIndexFixture(count int) []byte {
	return clusteredIndexFixture(count, false)
}

func clusteredIndexFixture(count int, complete bool) []byte {
	field := func(id, value int64) []byte {
		var b [4]byte
		binary.BigEndian.PutUint32(b[:], uint32(value))
		return ebmlWrap(id, b[:])
	}
	header := ebmlWrap(0x1a45dfa3, nil)
	info := ebmlWrap(0x1549a966, field(0x2ad7b1, 1000000))
	tracks := ebmlWrap(0x1654ae6b, ebmlWrap(0xae, bytes.Join([][]byte{field(0xd7, 7), field(0x83, 17), ebmlWrap(0x86, []byte("S_TEXT/UTF8"))}, nil)))
	clusters := make([][]byte, count)
	video := ebmlWrap(0xa3, append([]byte{0x81, 0, 0, 0x80}, make([]byte, 65536)...))
	for i := range count {
		body := field(0xe7, int64(i*2000))
		for range 5 {
			body = append(body, video...)
		}
		body = append(body, ebmlWrap(0xa0, append(ebmlWrap(0xa1, append([]byte{0x87, 0, 1, 0}, []byte("Sparse cue")...)), field(0x9b, 2000)...))...)
		clusters[i] = ebmlWrap(0x1f43b675, body)
	}
	cues := func(offset int64) []byte {
		var points []byte
		for i, cluster := range clusters {
			fields := append(field(0xf7, 7), field(0xf1, offset)...)
			if complete {
				fields = append(fields, field(0xf0, int64(len(field(0xe7, 0))+5*len(video)))...)
				fields = append(fields, field(0xb2, 2000)...)
			}
			position := ebmlWrap(0xb7, fields)
			points = append(points, ebmlWrap(0xbb, append(field(0xb3, int64(i*2000+1)), position...))...)
			offset += int64(len(cluster))
		}
		return ebmlWrap(0x1c53bb6b, points)
	}
	indexBytes := cues(int64(len(info) + len(tracks) + len(cues(0))))
	segment := bytes.Join(append([][]byte{info, tracks, indexBytes}, clusters...), nil)
	return append(header, ebmlWrap(0x18538067, segment)...)
}

func TestMatroskaSparseWindowBudget(t *testing.T) {
	const count = 150
	b := &indexedRanges{data: sparseIndexFixture(count)}
	r := b.reader(context.Background())
	index, err := loadMatroskaIndex(r)
	if err != nil {
		t.Fatal(err)
	}
	failure := errors.New("interrupted sparse read")
	failed := b.reader(context.Background())
	failed.backend = &failingIndexRanges{source: b, reads: 30, err: failure}
	if _, err := index.window(failed, 0, "vtt", 0, 300); !errors.Is(err, failure) {
		t.Fatal("masked sparse failure", err)
	}
	for _, cue := range index.tracks[0].cues {
		if (cue.relative < 0) != (cue.duration < 0) {
			t.Fatal("failed window retained an incomplete location")
		}
	}
	data, err := index.window(r, 0, "vtt", 0, 300)
	if err != nil || bytes.Count(data, []byte("Sparse cue")) != count {
		t.Fatal("incomplete sparse window", err)
	}
	if b.bytes >= 20<<20 || b.calls <= 512 || b.calls >= 8192 {
		t.Fatalf("sparse fallback bounds: %d bytes / %d ranges", b.bytes, b.calls)
	}
	t.Logf("300s sparse window: %d bytes / %d ranges; AV source %d bytes", b.bytes, b.calls, len(b.data))
	previous := b.bytes
	if _, err := index.window(b.reader(context.Background()), 0, "vtt", 0, 300); err != nil || b.bytes-previous > 32<<10 {
		t.Fatal("resolved positions were not reused", b.bytes-previous, err)
	}
	r.bytes = 128 << 20
	r.buffer = nil
	if _, err := r.read(0, 1); !errors.Is(err, errMatroskaIndex) {
		t.Fatal("fallback byte budget", err)
	}
	r.bytes, r.requests = 0, 8192
	if _, err := r.read(0, 1); !errors.Is(err, errMatroskaIndex) {
		t.Fatal("fallback request budget", err)
	}
}

type parallelIndexRanges struct {
	source                 *indexedRanges
	mu                     sync.Mutex
	arrivals, active, peak int
	gate                   chan struct{}
}

func (b *parallelIndexRanges) OpenRange(ctx context.Context, hash string, file engine.File, start, end int64) (io.ReadCloser, error) {
	b.mu.Lock()
	b.arrivals++
	arrival := b.arrivals
	b.active++
	b.peak = max(b.peak, b.active)
	if arrival == 4 {
		close(b.gate)
	}
	b.mu.Unlock()
	defer func() { b.mu.Lock(); b.active--; b.mu.Unlock() }()
	if arrival <= 4 {
		select {
		case <-b.gate:
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	return b.source.OpenRange(ctx, hash, file, start, end)
}

func TestMatroskaParallelWindowContracts(t *testing.T) {
	for _, complete := range []bool{false, true} {
		t.Run(fmt.Sprint("complete=", complete), func(t *testing.T) {
			testMatroskaParallelWindow(t, complete)
		})
	}
}

func testMatroskaParallelWindow(t *testing.T, complete bool) {
	b := &indexedRanges{data: clusteredIndexFixture(8, complete)}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	index, err := loadMatroskaIndex(b.reader(ctx))
	if err != nil {
		t.Fatal(err)
	}
	parallel := &parallelIndexRanges{source: b, gate: make(chan struct{})}
	r := b.reader(ctx)
	r.backend = parallel
	data, err := index.window(r, 0, "vtt", 0, 16)
	if err != nil || bytes.Count(data, []byte("Sparse cue")) != 8 || parallel.peak != 4 || parallel.active != 0 {
		t.Fatalf("parallel extraction: peak=%d active=%d error=%v", parallel.peak, parallel.active, err)
	}
	if complete {
		sequential, err := index.indexedClusters(b.reader(ctx), &index.tracks[0], 0, 16)
		if err != nil || !bytes.HasSuffix(data, sequential) {
			t.Fatal("concurrency changed subtitle blocks or their order", err)
		}
	}
	// Every concurrent read consumes the same original job budget.
	maxBytes, maxRequests, readSize := 128<<20, 8192, 16<<10
	if complete {
		maxBytes, maxRequests, readSize = 8<<20, 512, 16
	}
	for _, limit := range []string{"bytes", "requests"} {
		index, err := loadMatroskaIndex(b.reader(ctx))
		if err != nil {
			t.Fatal(err)
		}
		r := b.reader(ctx)
		if limit == "bytes" {
			r.bytes = int64(maxBytes - readSize)
		} else {
			r.requests = maxRequests - 1
		}
		previousBytes, previousCalls := b.bytes, b.calls
		if _, err = index.window(r, 0, "vtt", 0, 16); !errors.Is(err, errMatroskaIndex) {
			t.Fatal(limit, err)
		}
		if r.bytes > int64(maxBytes) || r.requests > maxRequests || b.bytes-previousBytes > readSize || b.calls-previousCalls > 1 {
			t.Fatal("per-worker budgets multiplied", limit)
		}
		for _, cue := range index.tracks[0].cues {
			if !complete && (cue.relative != -1 || cue.duration != -1) {
				t.Fatal("failed parallel window published offsets")
			}
		}
		// A new request can retry the same index after failure.
		if _, err = index.window(b.reader(ctx), 0, "vtt", 0, 16); err != nil {
			t.Fatal("retry", err)
		}
	}
}

func TestCaptionProgressDelayValidation(t *testing.T) {
	probe := &Probe{Duration: 100}
	p := &Playback{segments: []segment{{Start: 0, End: 100}}, status: PlaybackStatus{Timeline: SourceTimeline{WindowEnd: 100, Duration: 100}}, captions: &captions{probe: probe}}
	for _, delay := range []float64{-60, 0, 60} {
		if err := p.Confirm(10, delay); err != nil || p.captions.delay == nil || *p.captions.delay != delay {
			t.Fatal("supported delay rejected", delay, err)
		}
	}
	for _, delay := range []float64{-60.1, 60.1, math.NaN(), math.Inf(1)} {
		if err := p.Confirm(20, delay); err == nil || p.position != 10 || *p.captions.delay != 60 {
			t.Fatal("invalid delay mutated progress", delay, err)
		}
	}
}

func TestMatroskaCompleteWindowCancellation(t *testing.T) {
	for _, abort := range []bool{false, true} {
		b := &indexedRanges{data: clusteredIndexFixture(8, true)}
		index, err := loadMatroskaIndex(b.reader(context.Background()))
		if err != nil {
			t.Fatal(err)
		}
		ctx, cancel := context.WithCancel(context.Background())
		if !abort {
			cancel()
			ctx, cancel = context.WithTimeout(context.Background(), 100*time.Millisecond)
		} else {
			time.AfterFunc(100*time.Millisecond, cancel)
		}
		r := b.reader(ctx)
		r.backend = stalledIndexClusters{source: b, after: index.segment + index.tracks[0].cues[1].cluster}
		data, err := index.window(r, 0, "vtt", 0, 16)
		cancel()
		if err == nil || len(data) != 0 {
			t.Fatal("interrupted complete window published partial text", err)
		}
		if _, err := index.window(b.reader(context.Background()), 0, "vtt", 0, 16); err != nil {
			t.Fatal("complete index stayed locked after cancellation", err)
		}
	}
}

type stalledIndexClusters struct {
	source *indexedRanges
	after  int64
}

func (b stalledIndexClusters) OpenRange(ctx context.Context, hash string, file engine.File, start, end int64) (io.ReadCloser, error) {
	if start >= b.after {
		<-ctx.Done()
		return nil, ctx.Err()
	}
	return b.source.OpenRange(ctx, hash, file, start, end)
}

func TestMatroskaTimeoutRetainsOnlyVerifiedClusters(t *testing.T) {
	for _, explicitAbort := range []bool{false, true} {
		b := &indexedRanges{data: sparseIndexFixture(8)}
		index, err := loadMatroskaIndex(b.reader(context.Background()))
		if err != nil {
			t.Fatal(err)
		}
		var ctx context.Context
		var cancel context.CancelFunc
		if explicitAbort {
			ctx, cancel = context.WithCancel(context.Background())
			time.AfterFunc(150*time.Millisecond, cancel)
		} else {
			ctx, cancel = context.WithTimeout(context.Background(), 150*time.Millisecond)
		}
		r := b.reader(ctx)
		r.backend = stalledIndexClusters{source: b, after: index.segment + index.tracks[0].cues[1].cluster}
		data, err := index.window(r, 0, "vtt", 8, 16)
		cancel()
		if err == nil || len(data) != 0 {
			t.Fatal("failed window published text", err)
		}
		first := index.tracks[0].cues[0]
		if (first.relative >= 0) != !explicitAbort {
			t.Fatal("timeout/explicit abort metadata policy", explicitAbort, first)
		}
		for _, cue := range index.tracks[0].cues[1:] {
			if cue.relative != -1 || cue.duration != -1 {
				t.Fatal("unfinished cluster was remembered")
			}
		}
		data, err = index.window(b.reader(context.Background()), 0, "vtt", 8, 16)
		if err != nil || bytes.Count(data, []byte("Sparse cue")) != 5 {
			t.Fatal("retry lost overlap", err, bytes.Count(data, []byte("Sparse cue")))
		}
	}
}
