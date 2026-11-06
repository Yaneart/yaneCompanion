package media

import (
	"context"
	"encoding/base64"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"yanecompanion/internal/engine"
	"yanecompanion/internal/locator"
	"yanecompanion/release"
)

// Explicit opt-in integration against real peers and unchanged guarded engine.
func TestTorrentMedia(t *testing.T) {
	fixture, executable := os.Getenv("YANE_TEST_TORRENT"), os.Getenv("YANE_TEST_EXECUTABLE")
	if fixture == "" || executable == "" {
		t.Skip("set YANE_TEST_TORRENT and YANE_TEST_EXECUTABLE")
	}
	raw, err := os.ReadFile(fixture)
	if err != nil {
		t.Fatal(err)
	}
	input, err := locator.Validate(locator.Locator{Kind: "metainfo", Base64: base64.StdEncoding.EncodeToString(raw)})
	if err != nil {
		t.Fatal(err)
	}
	bundle, err := release.Verify(filepath.Dir(executable))
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 150*time.Second)
	defer cancel()
	e, err := engine.Start(ctx, bundle.Engine, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer e.Close()
	if err = e.Admit(ctx, input); err != nil {
		t.Fatal(err)
	}
	defer func() {
		cleanup, finish := context.WithTimeout(context.Background(), 5*time.Second)
		defer finish()
		if err := e.Drop(cleanup, input.Hash()); err != nil {
			t.Error(err)
		}
	}()
	files, err := e.Files(ctx, input.Hash())
	if err != nil {
		t.Fatal(err)
	}
	var file engine.File
	for _, f := range files {
		if f.Length > file.Length {
			file = f
		}
	}
	service := &Service{Runner: New(bundle.FFmpeg, bundle.FFprobe), Backend: e, Directory: t.TempDir()}
	probe, err := service.Probe(ctx, input.Hash(), file)
	if err != nil {
		t.Fatal("actual torrent probe", err)
	}
	if len(probe.Audio) == 0 {
		t.Fatal("integration fixture needs audio")
	}
	client := []ClientMedia{{probe.Video.ContentType, true}, {`audio/mp4; codecs="mp4a.40.2"`, true}}
	for _, position := range []float64{0, 63.3} {
		p, err := service.Prepare(ctx, input.Hash(), file, probe, Target{AudioRef: probe.Audio[0].Ref, Output: "original", Position: position}, client)
		if err != nil {
			t.Fatal("actual torrent preparation", position, err)
		}
		status := p.Status()
		if status.Timeline.AbsoluteAnchor > position+1 || status.Timeline.WindowEnd <= position || status.Bytes == 0 {
			p.Close()
			t.Fatalf("invalid measured timeline %+v", status)
		}
		var fragments []byte
		p.mu.Lock()
		init, err := p.root.ReadFile("init.mp4")
		if err != nil {
			p.mu.Unlock()
			p.Close()
			t.Fatal(err)
		}
		fragments = append(fragments, init...)
		for _, segment := range p.segments {
			bytes, err := p.root.ReadFile(segment.Name)
			if err != nil {
				p.mu.Unlock()
				p.Close()
				t.Fatal(err)
			}
			fragments = append(fragments, bytes...)
		}
		p.mu.Unlock()
		p.Close()
		path := filepath.Join(t.TempDir(), "prepared.mp4")
		if err = os.WriteFile(path, fragments, 0600); err != nil {
			t.Fatal(err)
		}
		// Decode prepared video/audio rather than accepting a nonempty manifest.
		if _, err = service.Runner.Prepare(ctx, []string{"-v", "error", "-nostdin", "-i", path, "-t", "2", "-f", "null", "-"}); err != nil {
			t.Fatal("prepared media decode", err)
		}
		t.Logf("torrent %s: position=%.1f anchor=%.6f mediaAnchor=%.6f edge=%.6f cache=%d video=%s audio=%s/%s", strings.Split(file.Path, "/")[0], position, status.Timeline.AbsoluteAnchor, status.Timeline.MediaAnchor, status.Timeline.WindowEnd, status.Bytes, status.Output.Video.Codec, status.Output.Audio.Codec, status.Output.Audio.Mode)
	}
}
