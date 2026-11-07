package media

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"io"
	"math"
	"os"
	"path/filepath"
	"testing"
	"time"

	"yanecompanion/release"
	"yanecompanion/test/fixture"
)

func TestRealAudioTailClocks(t *testing.T) {
	exe := os.Getenv("YANE_TEST_EXECUTABLE")
	if exe == "" {
		t.Skip("requires bundled tools")
	}
	bundle, err := release.Verify(filepath.Dir(exe))
	if err != nil {
		t.Fatal(err)
	}
	path := fixture.Tracks(t, bundle.FFmpeg, "testdata/tiny-h264.mp4")
	runner := New(bundle.FFmpeg, bundle.FFprobe)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	data, err := runner.Prepare(ctx, []string{"-v", "error", "-nostdin", "-ss", "190", "-noaccurate_seek", "-i", path, "-avoid_negative_ts", "make_zero", "-map", "0:v:0", "-c:v", "copy", "-map", "0:a:1", "-c:a", "aac", "-profile:a", "aac_low", "-b:a", "192k", "-ac", "2", "-ar", "48000", "-sn", "-dn", "-threads", "2", "-f", "mp4", "-strict", "experimental", "-movflags", "empty_moov+default_base_moof+frag_keyframe", "-frag_duration", "2000000", "-min_frag_duration", "1000000", "pipe:1"})
	if err != nil {
		t.Fatal(err)
	}
	reader := bytes.NewReader(data)
	var init []byte
	var clock mp4Clock
	var videoEnd float64
	tail := false
	tailOffset := 0
	var audioMoof, videoMoof []byte
	for {
		box, kind, err := readBox(reader)
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		switch kind {
		case "ftyp", "moov":
			init = append(init, box...)
			if kind == "moov" {
				clock, err = initClock(init)
				if err != nil {
					t.Fatal(err)
				}
			}
		case "moof":
			timing, err := clock.fragment(box)
			if errors.Is(err, errAudioOnly) {
				tail = true
				audioMoof = append([]byte(nil), box...)
				tailOffset = len(data) - reader.Len() - len(box)
			} else if err != nil {
				t.Fatal(err)
			} else {
				videoEnd = timing.end
				videoMoof = append([]byte(nil), box...)
			}
		}
	}
	if !tail {
		t.Fatal("fixture no longer produces a final AAC-only fragment")
	}
	consume := func(input []byte, anchor, duration float64) (*Playback, error) {
		t.Helper()
		root, err := os.OpenRoot(t.TempDir())
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { root.Close() })
		p := &Playback{ctx: ctx, root: root, input: &inputBridge{}, ready: make(chan struct{}), position: anchor, window: 45, status: PlaybackStatus{Timeline: SourceTimeline{AbsoluteAnchor: anchor, WindowEnd: anchor, Duration: duration}}}
		err = p.consume(ctx, bytes.NewReader(input))
		return p, err
	}
	p, err := consume(data, 190, 192)
	if err != nil || len(p.segments) != 1 {
		t.Fatal("valid audio flush rejected", err)
	}
	segment, err := p.root.ReadFile(p.segments[0].Name)
	if err != nil {
		t.Fatal(err)
	}
	parts, err := boxes(segment)
	if err != nil {
		t.Fatal(err)
	}
	moofs := 0
	for _, part := range parts {
		if part.kind == "moof" {
			moofs++
		}
	}
	if moofs != 2 || math.Abs(p.status.Timeline.WindowEnd-(190+videoEnd)) > 1e-6 {
		t.Fatal("lost audio or invented video time")
	}
	if _, err := consume(data, 0, 192); err == nil {
		t.Fatal("mid-file audio-only fragment accepted")
	}
	if _, err := consume(append(append([]byte(nil), data...), videoMoof...), 190, 192); err == nil {
		t.Fatal("moof after final tail accepted")
	}
	oversized := append(append([]byte(nil), data[:tailOffset]...), audioMoof...)
	large := make([]byte, (64<<10)+8)
	binary.BigEndian.PutUint32(large, uint32(len(large)))
	copy(large[4:], "mdat")
	oversized = append(oversized, large...)
	if _, err := consume(oversized, 190, 192); err == nil {
		t.Fatal("oversized audio tail accepted")
	}
}
