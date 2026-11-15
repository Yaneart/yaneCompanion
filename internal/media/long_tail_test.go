package media

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"io"
	"os"
	"path/filepath"
	"testing"
	"time"

	"yanecompanion/internal/engine"
	"yanecompanion/release"
)

func TestKnownAudioTailHonorsPausedWindowBudget(t *testing.T) {
	exe := os.Getenv("YANE_TEST_EXECUTABLE")
	if exe == "" {
		t.Skip("requires bundled tools")
	}
	bundle, err := release.Verify(filepath.Dir(exe))
	if err != nil {
		t.Fatal(err)
	}
	runner := New(bundle.FFmpeg, bundle.FFprobe)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	data, err := runner.Prepare(ctx, []string{"-v", "error", "-nostdin", "-i", "testdata/tiny-h264.mp4", "-stream_loop", "-1", "-i", waveFixture(t), "-map", "0:v:0", "-map", "1:a:0", "-c:v", "copy", "-c:a", "aac", "-t", "100", "-sn", "-dn", "-map_chapters", "-1", "-f", "mp4", "-movflags", "empty_moov+default_base_moof+frag_keyframe", "-frag_duration", "2000000", "-min_frag_duration", "1000000", "pipe:1"})
	if err != nil {
		t.Fatal("fixture", err)
	}
	for _, tc := range []struct {
		name   string
		window float64
	}{{"lookahead", 8}, {"segment count", 1000}} {
		t.Run(tc.name, func(t *testing.T) {
			root, err := os.OpenRoot(t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			defer root.Close()
			work, stop := context.WithCancel(ctx)
			defer stop()
			p := &Playback{ctx: work, root: root, input: &inputBridge{}, ready: make(chan struct{}), position: 22, window: tc.window, videoEnd: 24, audioEnd: 100, status: PlaybackStatus{Timeline: SourceTimeline{Duration: 100}}}
			result := make(chan error, 1)
			go func() { result <- p.consume(work, bytes.NewReader(data)) }()
			defer func() {
				stop()
				select {
				case <-result:
				case <-time.After(time.Second):
					t.Error("consumer ignored cancellation")
				}
			}()
			deadline := time.Now().Add(2 * time.Second)
			blocked := false
			for time.Now().Before(deadline) {
				select {
				case err := <-result:
					result <- err
					t.Fatalf("paused tail ran beyond its budget to end=%f: %v", p.Status().Timeline.WindowEnd, err)
				default:
				}
				p.mu.Lock()
				blocked = p.blocked()
				p.mu.Unlock()
				if blocked {
					break
				}
				time.Sleep(10 * time.Millisecond)
			}
			if !blocked {
				t.Fatal("audio tail did not fill its bounded window")
			}
			first := p.Status()
			p.mu.Lock()
			count := len(p.segments)
			p.mu.Unlock()
			if tc.window == 8 && first.Timeline.WindowEnd > 32.1 || tc.window == 1000 && count != 32 {
				t.Fatalf("audio tail passed its budget: window=%f segments=%d end=%f", tc.window, count, first.Timeline.WindowEnd)
			}
			time.Sleep(150 * time.Millisecond)
			if after := p.Status(); after.Timeline.WindowEnd != first.Timeline.WindowEnd || after.Bytes > cacheLimit {
				t.Fatalf("paused audio tail kept consuming: before=%+v after=%+v", first, after)
			}
		})
	}
}

func TestCopiedAudioSharesMuxTimescale(t *testing.T) {
	exe := os.Getenv("YANE_TEST_EXECUTABLE")
	if exe == "" {
		t.Skip("requires bundled tools")
	}
	bundle, err := release.Verify(filepath.Dir(exe))
	if err != nil {
		t.Fatal(err)
	}
	runner := New(bundle.FFmpeg, bundle.FFprobe)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	path := filepath.Join(t.TempDir(), "copied-44100.mkv")
	_, err = runner.Prepare(ctx, []string{"-v", "error", "-nostdin", "-i", "testdata/tiny-h264.mp4", "-stream_loop", "-1", "-i", waveFixture(t), "-map", "0:v:0", "-map", "1:a:0", "-c:v", "copy", "-c:a", "aac", "-ar", "44100", "-t", "28", path})
	if err != nil {
		t.Fatal("fixture", err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	file := engine.File{Index: 1, Path: "copied.mkv", Length: info.Size()}
	service := &Service{Runner: runner, Backend: fileRanges{path}, Directory: t.TempDir()}
	probe, err := service.Probe(ctx, "hash", file)
	if err != nil {
		t.Fatal(err)
	}
	if len(probe.Audio) != 1 || probe.Audio[0].SampleRate != 44100 {
		t.Fatal("fixture audio rate")
	}
	p, err := service.Prepare(ctx, "hash", file, probe, Target{AudioRef: probe.Audio[0].Ref, Position: 27.8, Output: "original"}, []ClientMedia{{probe.Video.ContentType, true}, {`audio/mp4; codecs="mp4a.40.2"`, true}})
	if err != nil {
		t.Fatal(err)
	}
	defer p.Close()
	select {
	case <-p.done:
	case <-ctx.Done():
		t.Fatal("EOF deadline")
	}
	status := p.Status()
	if status.Error != "" || !status.Timeline.Ended || status.Output.Audio == nil || status.Output.Audio.Mode != "copy" {
		t.Fatalf("copied audio EOF: %+v", status)
	}
	init, err := p.root.ReadFile("init.mp4")
	if err != nil {
		t.Fatal(err)
	}
	clock, err := initClock(init)
	if err != nil {
		t.Fatal(err)
	}
	if len(clock.timescales) != 2 {
		t.Fatal("fixture must retain both AV tracks")
	}
	for _, scale := range clock.timescales {
		if scale != 44100 {
			t.Fatalf("copied audio clock changed to %d", scale)
		}
	}
}

func TestKnownAudioTailRejectsInvalidBoundaries(t *testing.T) {
	exe := os.Getenv("YANE_TEST_EXECUTABLE")
	if exe == "" {
		t.Skip("requires bundled tools")
	}
	bundle, err := release.Verify(filepath.Dir(exe))
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	runner := New(bundle.FFmpeg, bundle.FFprobe)
	data, err := runner.Prepare(ctx, []string{"-v", "error", "-nostdin", "-i", "testdata/tiny-h264.mp4", "-stream_loop", "-1", "-i", waveFixture(t), "-map", "0:v:0", "-map", "1:a:0", "-c:v", "copy", "-c:a", "aac", "-t", "28", "-sn", "-dn", "-map_chapters", "-1", "-f", "mp4", "-movflags", "empty_moov+default_base_moof+frag_keyframe", "-frag_duration", "2000000", "-min_frag_duration", "1000000", "pipe:1"})
	if err != nil {
		t.Fatal("fixture", err)
	}
	reader := bytes.NewReader(data)
	var init, videoFragment, expectedFragments []byte
	var clock mp4Clock
	tailOffset := -1
	var tailMoof []byte
	for {
		box, kind, err := readBox(reader)
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		if kind == "ftyp" || kind == "moov" {
			init = append(init, box...)
			if kind == "moov" {
				clock, err = initClock(init)
				if err != nil {
					t.Fatal(err)
				}
			}
		}
		if kind == "moof" {
			offset := len(data) - reader.Len() - len(box)
			_, clockErr := clock.fragment(box)
			mdat, kind, err := readBox(reader)
			if err != nil || kind != "mdat" {
				t.Fatal("fixture fragment", err)
			}
			expectedFragments = append(expectedFragments, box...)
			expectedFragments = append(expectedFragments, mdat...)
			if errors.Is(clockErr, errAudioOnly) && tailOffset < 0 {
				tailOffset, tailMoof = offset, box
			} else if clockErr == nil {
				videoFragment = append(append([]byte(nil), box...), mdat...)
			}
		}
	}
	if tailOffset < 0 || len(videoFragment) == 0 {
		t.Fatal("fixture lacks separate audio tail")
	}
	shifted := func(seconds int64) []byte {
		copyData := append([]byte(nil), data...)
		trackAt := bytes.Index(tailMoof, []byte("tfhd"))
		timeAt := bytes.Index(tailMoof, []byte("tfdt"))
		if trackAt < 0 || timeAt < 0 {
			t.Fatal("fixture lacks decode clock")
		}
		track := binary.BigEndian.Uint32(tailMoof[trackAt+8:])
		delta := seconds * int64(clock.timescales[track])
		at := tailOffset + timeAt + 8
		if tailMoof[timeAt+4] == 1 {
			binary.BigEndian.PutUint64(copyData[at:], uint64(int64(binary.BigEndian.Uint64(copyData[at:]))+delta))
		} else {
			binary.BigEndian.PutUint32(copyData[at:], uint32(int64(binary.BigEndian.Uint32(copyData[at:]))+delta))
		}
		return copyData
	}
	for _, tc := range []struct {
		name               string
		input              []byte
		videoEnd, audioEnd float64
		valid              bool
	}{
		{"known tail", data, 24, 28, true},
		{"unknown video", data, 0, 28, false},
		{"unknown audio", data, 24, 0, false},
		{"premature audio", data, 27, 28, false},
		{"past selected audio end", data, 24, 25, false},
		{"video after tail", append(append([]byte(nil), data...), videoFragment...), 24, 28, false},
		{"decode gap", shifted(1), 24, 28, false},
		{"decode overlap", shifted(-1), 24, 28, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root, err := os.OpenRoot(t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			defer root.Close()
			p := &Playback{ctx: ctx, root: root, input: &inputBridge{}, ready: make(chan struct{}), position: 24, window: 45, videoEnd: tc.videoEnd, audioEnd: tc.audioEnd, status: PlaybackStatus{Timeline: SourceTimeline{Duration: 28}}}
			err = p.consume(ctx, bytes.NewReader(tc.input))
			if (err == nil) != tc.valid {
				t.Fatalf("valid=%t err=%v", tc.valid, err)
			}
			if tc.valid && p.Status().Timeline.WindowEnd < 27.9 {
				t.Fatal("lost real audio tail")
			}
			if tc.valid {
				var actual []byte
				for _, segment := range p.segments {
					data, err := p.root.ReadFile(segment.Name)
					if err != nil {
						t.Fatal(err)
					}
					actual = append(actual, data...)
				}
				if !bytes.Equal(actual, expectedFragments) {
					t.Fatal("published sample bytes changed or were lost")
				}
			}
		})
	}
}
