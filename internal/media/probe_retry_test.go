package media

import (
	"context"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"yanecompanion/internal/engine"
	"yanecompanion/release"
)

func TestProbeRetrySharesInputBudget(t *testing.T) {
	path := filepath.Join(t.TempDir(), "input")
	if err := os.WriteFile(path, make([]byte, 16), 0600); err != nil {
		t.Fatal(err)
	}
	budget := &inputBudget{limit: 16}
	for attempt := 0; attempt < 3; attempt++ {
		b, err := bridge(context.Background(), fileRanges{path}, "fixture", engine.File{Length: 16}, 16, budget)
		if err != nil {
			t.Fatal(err)
		}
		req, _ := http.NewRequest("GET", b.url, nil)
		req.Header.Set("Range", "bytes=0-7")
		response, err := http.DefaultClient.Do(req)
		if err != nil {
			b.Close()
			t.Fatal(err)
		}
		data, readErr := io.ReadAll(response.Body)
		response.Body.Close()
		b.Close()
		if attempt < 2 && (readErr != nil || len(data) != 8) {
			t.Fatalf("attempt%d bytes=%d err=%v", attempt, len(data), readErr)
		}
		if attempt == 2 && (!b.exceeded.Load() || len(data) != 0 || readErr == nil) {
			t.Fatal("retry reset shared input budget")
		}
	}
}

type interruptedProbeRanges struct {
	RangeBackend
	calls  atomic.Int64
	always bool
	stall  bool
}

func (b *interruptedProbeRanges) OpenRange(ctx context.Context, hash string, file engine.File, start, end int64) (io.ReadCloser, error) {
	call := b.calls.Add(1)
	if call == 1 && b.stall {
		<-ctx.Done()
		return nil, ctx.Err()
	}
	if (call == 1 && !b.stall) || b.always {
		return nil, io.ErrUnexpectedEOF
	}
	return b.RangeBackend.OpenRange(ctx, hash, file, start, end)
}

func TestRealProbeRetriesWorkerDeadline(t *testing.T) {
	exe := os.Getenv("YANE_TEST_EXECUTABLE")
	if exe == "" {
		t.Skip("requires bundled ffprobe")
	}
	bundle, err := release.Verify(filepath.Dir(exe))
	if err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat("testdata/tiny-h264.mp4")
	if err != nil {
		t.Fatal(err)
	}
	for _, canceled := range []bool{false, true} {
		t.Run(map[bool]string{false: "worker timeout", true: "caller cancellation"}[canceled], func(t *testing.T) {
			backend := &interruptedProbeRanges{RangeBackend: fileRanges{"testdata/tiny-h264.mp4"}, stall: true}
			s := &Service{Runner: New(bundle.FFmpeg, bundle.FFprobe), Backend: backend, Directory: t.TempDir()}
			duration := 40 * time.Second
			if canceled {
				duration = 100 * time.Millisecond
			}
			ctx, cancel := context.WithTimeout(context.Background(), duration)
			defer cancel()
			p, err := s.Probe(ctx, "fixture", engine.File{Length: info.Size(), Path: "fixture.mp4"})
			if canceled {
				if err == nil || backend.calls.Load() != 1 {
					t.Fatalf("canceled probe retried: %v calls=%d", err, backend.calls.Load())
				}
			} else if err != nil || p == nil || backend.calls.Load() < 2 {
				t.Fatalf("worker deadline recovery failed: %v calls=%d", err, backend.calls.Load())
			}
		})
	}
}

func TestRealProbeRetriesInterruptedTorrentRange(t *testing.T) {
	exe := os.Getenv("YANE_TEST_EXECUTABLE")
	if exe == "" {
		t.Skip("requires bundled ffprobe")
	}
	bundle, err := release.Verify(filepath.Dir(exe))
	if err != nil {
		t.Fatal(err)
	}
	path := "testdata/tiny-h264.mp4"
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	file := engine.File{Index: 1, Path: "fixture.mp4", Length: info.Size()}
	for _, always := range []bool{false, true} {
		t.Run(map[bool]string{false: "recovers", true: "bounded failure"}[always], func(t *testing.T) {
			backend := &interruptedProbeRanges{RangeBackend: fileRanges{path}, always: always}
			s := &Service{Runner: New(bundle.FFmpeg, bundle.FFprobe), Backend: backend, Directory: t.TempDir()}
			ctx, cancel := context.WithTimeout(context.Background(), 12*time.Second)
			defer cancel()
			p, err := s.Probe(ctx, "fixture", file)
			if always {
				if err == nil || err.Error() != "NO_PEERS" || backend.calls.Load() > 4 {
					t.Fatalf("unbounded/incorrect failure: %v calls=%d", err, backend.calls.Load())
				}
			} else if err != nil || p == nil || p.Duration <= 0 {
				t.Fatalf("range retry failed: %v calls=%d", err, backend.calls.Load())
			}
			if backend.calls.Load() < 2 {
				t.Fatal("fixture did not exercise recovery")
			}
		})
	}
}
