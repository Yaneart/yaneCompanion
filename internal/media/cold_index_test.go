package media

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"yanecompanion/internal/engine"
	"yanecompanion/internal/locator"
	"yanecompanion/release"
)

type coldIndexBackend struct {
	RangeBackend
	t                    *testing.T
	reads, bytes, active atomic.Int64
	peak                 atomic.Int64
}

type coldIndexBody struct {
	io.ReadCloser
	owner *coldIndexBackend
	once  sync.Once
}

func (b *coldIndexBody) Read(p []byte) (int, error) {
	n, err := b.ReadCloser.Read(p)
	b.owner.bytes.Add(int64(n))
	return n, err
}

func (b *coldIndexBody) Close() error {
	var err error
	b.once.Do(func() { err = b.ReadCloser.Close(); b.owner.active.Add(-1) })
	return err
}

func (b *coldIndexBackend) OpenRange(ctx context.Context, hash string, file engine.File, start, end int64) (io.ReadCloser, error) {
	b.reads.Add(1)
	active := b.active.Add(1)
	for peak := b.peak.Load(); active > peak && !b.peak.CompareAndSwap(peak, active); peak = b.peak.Load() {
	}
	began := time.Now()
	body, err := b.RangeBackend.OpenRange(ctx, hash, file, start, end)
	if elapsed := time.Since(began); elapsed > time.Second {
		b.t.Logf("range %d..%d: headers after %s, error=%v", start, end, elapsed.Round(time.Millisecond), err)
	}
	if err != nil {
		b.active.Add(-1)
		return nil, err
	}
	return &coldIndexBody{ReadCloser: body, owner: b}, nil
}

// Each case starts an independent real engine with an empty profile/cache.
// Input is private JSON: locator, fileIndex, stream and positions. It is never
// logged. The sequential control preserves the previous complete-index path;
// its framing and FFmpeg decoder are identical to the concurrent path.
func TestTorrentColdIndexedWindow(t *testing.T) {
	inputPath, exe := os.Getenv("YANE_CAPTION_REVIEW"), os.Getenv("YANE_TEST_EXECUTABLE")
	if inputPath == "" || exe == "" {
		t.Skip("opt-in peer-backed cold index measurement")
	}
	var spec struct {
		Locator   locator.Locator `json:"locator"`
		FileIndex int             `json:"fileIndex"`
		Stream    int             `json:"stream"`
		Positions []float64       `json:"positions"`
	}
	raw, err := os.ReadFile(inputPath)
	if err != nil || json.Unmarshal(raw, &spec) != nil {
		t.Fatal("invalid private review input")
	}
	input, err := locator.Validate(spec.Locator)
	if err != nil {
		t.Fatal(err)
	}
	bundle, err := release.Verify(filepath.Dir(exe))
	if err != nil {
		t.Fatal(err)
	}
	for _, position := range spec.Positions {
		for _, parallel := range []bool{false, true} {
			t.Run(fmt.Sprintf("position=%.0f/parallel=%t", position, parallel), func(t *testing.T) {
				ctx, cancel := context.WithTimeout(context.Background(), 150*time.Second)
				defer cancel()
				e, err := engine.Start(ctx, bundle.Engine, t.TempDir())
				if err != nil {
					t.Fatal(err)
				}
				defer e.Close()
				began := time.Now()
				if err := e.Admit(ctx, input); err != nil {
					t.Fatal("metadata admission", err)
				}
				files, err := e.Files(ctx, input.Hash())
				for len(files) == 0 && err == nil && ctx.Err() == nil {
					select {
					case <-ctx.Done():
					case <-time.After(250 * time.Millisecond):
					}
					files, err = e.Files(ctx, input.Hash())
				}
				if err != nil {
					t.Fatal(err)
				}
				var file engine.File
				for _, candidate := range files {
					if candidate.Index == spec.FileIndex {
						file = candidate
					}
				}
				if file.Length == 0 {
					t.Fatal("file not found")
				}
				t.Logf("metadata=%s fileBytes=%d", time.Since(began).Round(time.Millisecond), file.Length)
				resourceCtx, stop := context.WithTimeout(ctx, 30*time.Second)
				defer stop()
				backend := &coldIndexBackend{RangeBackend: e, t: t}
				r := &matroskaReader{ctx: resourceCtx, backend: backend, hash: input.Hash(), file: file}
				began = time.Now()
				index, err := loadMatroskaIndex(r)
				if err != nil {
					t.Fatalf("cold index after %s: %v; reads=%d bytes=%d active=%d", time.Since(began), err, backend.reads.Load(), backend.bytes.Load(), backend.active.Load())
				}
				t.Logf("cold index=%s reads=%d bytes=%d", time.Since(began).Round(time.Millisecond), r.requests, r.bytes)
				var track *indexedTrack
				for i := range index.tracks {
					if index.tracks[i].stream == spec.Stream {
						track = &index.tracks[i]
					}
				}
				if track == nil {
					t.Fatal("selected track missing")
				}
				var data []byte
				if parallel {
					data, err = index.window(r, spec.Stream, track.format, position, position+30)
				} else {
					data, err = index.indexedClusters(r, track, position, position+30)
					if err == nil && len(data) > 0 {
						header := append([]byte(nil), index.header...)
						header = append(header, 0x18, 0x53, 0x80, 0x67, 0x01, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff)
						header = append(header, index.info...)
						header = append(header, ebmlWrap(0x1654ae6b, track.entry)...)
						data = append(header, data...)
					}
				}
				if err == nil && len(data) > 0 {
					format := map[string]string{"vtt": "webvtt", "ass": "ass"}[track.format]
					runner := New(bundle.FFmpeg, bundle.FFprobe)
					data, err = runner.runInputLimit(resourceCtx, bundle.FFmpeg, []string{"-v", "error", "-nostdin", "-copyts", "-protocol_whitelist", "pipe", "-format_whitelist", "matroska,webm", "-i", "pipe:0", "-map", "0:0", "-c:s", format, "-f", format, "pipe:1"}, runner.resources, textLimit, data)
					if err == nil {
						data, err = subtitleWindow(data, track.format, position, position+30)
					}
				}
				t.Logf("cold total=%s reads=%d bytes=%d peak=%d active=%d decoded=%d error=%v", time.Since(began).Round(time.Millisecond), backend.reads.Load(), backend.bytes.Load(), backend.peak.Load(), backend.active.Load(), len(data), err)
				if err != nil || backend.active.Load() != 0 {
					t.Fail()
				}
			})
		}
	}
}
