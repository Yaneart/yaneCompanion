package api

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"yanecompanion/internal/engine"
	"yanecompanion/internal/locator"
	"yanecompanion/internal/media"
	"yanecompanion/internal/sessions"
	"yanecompanion/release"
	"yanecompanion/test/fixture"
)

type resilienceEngine struct {
	mediaEngine
	block, interrupted atomic.Bool
	active, bytes      atomic.Int64
	started            chan struct{}
}

type resilienceRead struct {
	io.ReadCloser
	e *resilienceEngine
}

func (r *resilienceRead) Read(p []byte) (int, error) {
	n, err := r.ReadCloser.Read(p)
	r.e.bytes.Add(int64(n))
	return n, err
}
func (r *resilienceRead) Close() error {
	r.e.active.Add(-1)
	return r.ReadCloser.Close()
}
func (e *resilienceEngine) OpenRange(ctx context.Context, hash string, f engine.File, start, end int64) (io.ReadCloser, error) {
	if e.block.Load() {
		select {
		case e.started <- struct{}{}:
		default:
		}
		<-ctx.Done()
		return nil, ctx.Err()
	}
	if e.interrupted.Swap(false) {
		return nil, io.ErrUnexpectedEOF
	}
	r, err := e.mediaEngine.OpenRange(ctx, hash, f, start, end)
	if err != nil {
		return nil, err
	}
	e.active.Add(1)
	return &resilienceRead{r, e}, nil
}

// Measures real media producers with controlled file-backed input. This is not
// an Internet swarm benchmark or a native installer/platform acceptance test.
func TestLC013PlaybackBudgets(t *testing.T) {
	exe := os.Getenv("YANE_TEST_EXECUTABLE")
	if exe == "" {
		t.Skip("requires bundled engine/FFmpeg")
	}
	bundle, err := release.Verify(filepath.Dir(exe))
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Minute)
	defer cancel()
	start := time.Now()
	worker, err := engine.Start(ctx, bundle.Engine, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("engine_start_ms=%d", time.Since(start).Milliseconds())
	worker.Close()
	runner := media.New(bundle.FFmpeg, bundle.FFprobe)
	e := &resilienceEngine{mediaEngine: mediaEngine{path: fixture.Tracks(t, bundle.FFmpeg, "../media/testdata/tiny-h264.mp4")}, started: make(chan struct{}, 1)}
	directory := t.TempDir()
	m := sessions.NewWithMedia(e, &media.Service{Runner: runner, Backend: e, Directory: directory})
	defer m.Close()
	c, _ := m.CreateController(website, "isolated-review")
	key := func(n int) string { return fmt.Sprintf("123e4567-e89b-42d3-a456-%012d", n) }
	a, err := m.Create(website, c.Token, key(1), []byte("create"), locator.Locator{Kind: "magnet", Value: "magnet:?xt=urn:btih:" + strings.Repeat("a", 40)})
	if err != nil {
		t.Fatal(err)
	}
	wait := func(state, operation string) sessions.Snapshot {
		t.Helper()
		for deadline := time.Now().Add(40 * time.Second); time.Now().Before(deadline); time.Sleep(10 * time.Millisecond) {
			s, err := m.Snapshot(website, c.Token, a.SessionID)
			if err != nil || s.Error != "" {
				t.Fatalf("snapshot: %v %s", err, s.Error)
			}
			if operation != "" {
				op, err := m.Operation(website, c.Token, operation)
				if err != nil || op.State == "failed" {
					t.Fatalf("operation: %+v %v", op, err)
				}
				if op.State != "ready" {
					continue
				}
			}
			if s.State == state {
				return s
			}
		}
		t.Fatal("session deadline")
		return sessions.Snapshot{}
	}
	s := wait("awaiting_file", "")
	command := func(n int, cmd sessions.Command, state string) sessions.Snapshot {
		t.Helper()
		cmd.Revision = s.Revision
		if _, err := m.Command(website, c.Token, s.ID, key(n), []byte(key(n)), cmd); err != nil {
			t.Fatal(err)
		}
		return wait(state, key(n))
	}
	start = time.Now()
	e.interrupted.Store(true)
	s = command(2, sessions.Command{Action: "probe", FileRef: s.Files[0].Ref}, "awaiting_target")
	t.Logf("probe_with_interrupted_range_ms=%d", time.Since(start).Milliseconds())
	target := media.Target{FileRef: s.Selected, AudioRef: s.Probe.Audio[0].Ref, Position: 0, Output: "original", AudioMode: "separate"}
	client := []media.ClientMedia{{ContentType: s.Probe.Video.ContentType, Supported: true}, {ContentType: `audio/mp4; codecs="mp4a.40.2"`, Supported: true}}
	start = time.Now()
	s = command(3, sessions.Command{Action: "configure", Target: &target, ClientMedia: client}, "ready")
	t.Logf("prepare_ms=%d cache_bytes=%d", time.Since(start).Milliseconds(), s.Source.Bytes)
	start = time.Now()
	target.Position = 72
	s = command(4, sessions.Command{Action: "configure", Target: &target, ClientMedia: client}, "ready")
	t.Logf("seek_72_ms=%d", time.Since(start).Milliseconds())
	videoID, videoRevision, videoURL := s.Source.ID, s.Source.Revision, s.Source.URL
	target.AudioRef = s.Probe.Audio[1].Ref
	start = time.Now()
	s = command(5, sessions.Command{Action: "audio", Target: &target}, "ready")
	t.Logf("audio_switch_ms=%d cache_bytes=%d", time.Since(start).Milliseconds(), s.Source.Bytes)
	if s.Source.ID != videoID || s.Source.Revision != videoRevision || s.Source.URL != videoURL || s.Source.Bytes > 128<<20 {
		t.Fatal("audio replaced video or exceeded aggregate budget")
	}
	// Stop while the next seek is blocked waiting for input. The pending receipt
	// must fail, the grant must revoke, and every worker/read/file must be joined.
	e.block.Store(true)
	target.Position = 120
	_, err = m.Command(website, c.Token, s.ID, key(6), []byte("blocked seek"), sessions.Command{Action: "configure", Revision: s.Revision, Target: &target, ClientMedia: client})
	if err != nil {
		t.Fatal(err)
	}
	select {
	case <-e.started:
	case <-ctx.Done():
		t.Fatal("seek did not request input")
	}
	start = time.Now()
	m.SetStorageAvailable(false)
	for deadline := time.Now().Add(3 * time.Second); m.Activity() != ""; time.Sleep(10 * time.Millisecond) {
		if time.Now().After(deadline) {
			t.Fatal("storage pressure did not join media")
		}
	}
	op, err := m.Operation(website, c.Token, key(6))
	entries, dirErr := os.ReadDir(directory)
	if err != nil || op.State != "failed" || op.Error != "CACHE_FULL" || e.active.Load() != 0 || dirErr != nil || len(entries) != 0 {
		t.Fatalf("cleanup failure: op=%+v reads=%d dirs=%d err=%v/%v", op, e.active.Load(), len(entries), err, dirErr)
	}
	t.Logf("pressure_cancel_ms=%d input_bytes=%d active_reads=%d media_directories=%d", time.Since(start).Milliseconds(), e.bytes.Load(), e.active.Load(), len(entries))
	if review := os.Getenv("YANE_LC013_REVIEW"); review != "" {
		m.SetStorageAvailable(true)
		e.block.Store(false)
		serveResilienceReview(t, m, review)
		m.Close()
		entries, err := os.ReadDir(directory)
		if err != nil || len(entries) != 0 || e.active.Load() != 0 {
			t.Fatal("browser review leaked media resources")
		}
		t.Logf("browser_cleanup active_reads=%d media_directories=%d input_bytes=%d", e.active.Load(), len(entries), e.bytes.Load())
	}
}

// Opt-in bounded no-peers failure with the real engine, no trackers/DHT/peers.
func TestLC013NoPeersDeadline(t *testing.T) {
	if os.Getenv("YANE_LC013_LONG") != "1" {
		t.Skip("opt-in 120 second metadata deadline")
	}
	bundle, err := release.Verify(filepath.Dir(os.Getenv("YANE_TEST_EXECUTABLE")))
	if err != nil {
		t.Fatal(err)
	}
	e, err := engine.Start(context.Background(), bundle.Engine, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer e.Close()
	m := sessions.New(e)
	defer m.Close()
	c, _ := m.CreateController(website, "isolated-no-peers")
	start := time.Now()
	a, err := m.Create(website, c.Token, "123e4567-e89b-42d3-a456-426614174000", []byte("no peers"), locator.Locator{Kind: "magnet", Value: "magnet:?xt=urn:btih:" + strings.Repeat("e", 40)})
	if err != nil {
		t.Fatal(err)
	}
	for deadline := time.Now().Add(135 * time.Second); time.Now().Before(deadline); time.Sleep(200 * time.Millisecond) {
		m.Tick()
		s, err := m.Snapshot(website, c.Token, a.SessionID)
		if err != nil {
			t.Fatal(err)
		}
		if s.State == "failed" {
			if s.Error != "NO_PEERS" {
				t.Fatal("unexpected failure", s.Error)
			}
			m.Close()
			if m.Activity() != "" {
				t.Fatal("failed admission retained device")
			}
			t.Logf("no_peers_ms=%d", time.Since(start).Milliseconds())
			return
		}
	}
	t.Fatal("metadata exceeded its deadline")
}
