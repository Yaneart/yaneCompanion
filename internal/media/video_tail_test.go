package media

import (
	"bytes"
	"context"
	"io"
	"os"
	"path/filepath"
	"testing"
	"time"

	"yanecompanion/release"
	"yanecompanion/test/fixture"
)

type tailObservingReader struct {
	*bytes.Reader
	observe func()
}

func (r tailObservingReader) Read(data []byte) (int, error) {
	n, err := r.Reader.Read(data)
	r.observe()
	return n, err
}

func TestRealMuxedVideoTailPacking(t *testing.T) {
	exe := os.Getenv("YANE_TEST_EXECUTABLE")
	if exe == "" {
		t.Skip("requires bundled tools")
	}
	bundle, err := release.Verify(filepath.Dir(exe))
	if err != nil {
		t.Fatal(err)
	}
	input := fixture.Tracks(t, bundle.FFmpeg, "testdata/tiny-h264.mp4")
	runner := New(bundle.FFmpeg, bundle.FFprobe)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	for _, seconds := range []string{"8", "14"} {
		t.Run(seconds, func(t *testing.T) {
			data, err := runner.Prepare(ctx, []string{"-v", "error", "-nostdin", "-i", input, "-t", "2", "-i", input, "-map", "0:v:0", "-map", "1:a:0", "-c:v", "copy", "-c:a", "aac", "-ac", "2", "-t", seconds, "-sn", "-dn", "-map_chapters", "-1", "-f", "mp4", "-movflags", "empty_moov+default_base_moof+frag_keyframe", "-frag_duration", "2000000", "-min_frag_duration", "1000000", "pipe:1"})
			if err != nil {
				t.Fatal(err)
			}
			root, err := os.OpenRoot(t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			defer root.Close()
			duration := 8.0
			if seconds == "14" {
				duration = 14
			}
			p := &Playback{ctx: ctx, root: root, input: &inputBridge{}, ready: make(chan struct{}), window: 45, videoEnd: duration, status: PlaybackStatus{Timeline: SourceTimeline{Duration: duration}, Output: OutputInfo{Audio: &OutputAudio{}}}}
			firstTarget := 0
			err = p.consume(ctx, tailObservingReader{bytes.NewReader(data), func() {
				if firstTarget == 0 && len(p.segments) > 0 {
					firstTarget = p.target
				}
			}})
			if seconds == "14" {
				if err == nil {
					t.Fatal("accepted an unbounded video-only tail")
				}
				return
			}
			if err != nil || len(p.segments) != 2 || p.segments[1].Duration <= 3 || p.segments[1].Duration > 8 || firstTarget != 8 || p.target != firstTarget {
				t.Fatalf("EOF tail was not packed: segments=%+v target=%d err=%v", p.segments, p.target, err)
			}
			var original, packed []byte
			reader := bytes.NewReader(data)
			for {
				box, kind, err := readBox(reader)
				if err == io.EOF {
					break
				}
				if err != nil {
					t.Fatal(err)
				}
				if kind == "moof" || kind == "mdat" {
					original = append(original, box...)
				}
			}
			for _, segment := range p.segments {
				part, err := p.root.ReadFile(segment.Name)
				if err != nil {
					t.Fatal(err)
				}
				packed = append(packed, part...)
			}
			if !bytes.Equal(original, packed) {
				t.Fatal("packing changed media samples or timestamps")
			}
			for _, bad := range []struct {
				name string
				end  float64
				data []byte
			}{
				{"unknown video end", 0, data},
				{"incomplete EOF", 9, data},
				{"audio returns", duration, data},
			} {
				// Append the first actual AV moof/mdat pair to test reappearance.
				if bad.name == "audio returns" {
					r := bytes.NewReader(original)
					moof, _, _ := readBox(r)
					mdat, _, _ := readBox(r)
					bad.data = append(append(append([]byte(nil), data...), moof...), mdat...)
				}
				t.Run(bad.name, func(t *testing.T) {
					root, err := os.OpenRoot(t.TempDir())
					if err != nil {
						t.Fatal(err)
					}
					defer root.Close()
					p := &Playback{ctx: ctx, root: root, input: &inputBridge{}, ready: make(chan struct{}), window: 45, videoEnd: bad.end, status: PlaybackStatus{Timeline: SourceTimeline{Duration: duration}, Output: OutputInfo{Audio: &OutputAudio{}}}}
					if err := p.consume(ctx, bytes.NewReader(bad.data)); err == nil {
						t.Fatal("accepted an unverified EOF tail")
					}
				})
			}
		})
	}
}
