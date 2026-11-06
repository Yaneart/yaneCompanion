package engine

import (
	"context"
	"encoding/base64"
	"net/url"
	"os"
	"path/filepath"
	"testing"
	"time"
	"yanecompanion/internal/locator"
	"yanecompanion/release"
)

// Opt-in real engine/peer delivery, with an isolated seeder or a public fixture.
// Metadata is supplied locally; no browser URL fetch or production bypass exists.
func TestTorrentPrefix(t *testing.T) {
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
		t.Fatal("metainfo validation", err)
	}
	inputs := map[string]locator.Input{"metainfo": input}
	for name, tracker := range map[string]string{"cold magnet": os.Getenv("YANE_TEST_MAGNET_TRACKER"), "cold UDP magnet": os.Getenv("YANE_TEST_UDP_TRACKER")} {
		if tracker == "" {
			continue
		}
		magnet, err := locator.Validate(locator.Locator{Kind: "magnet", Value: "magnet:?xt=urn:btih:" + input.Hash() + "&tr=" + url.QueryEscape(tracker)})
		if err != nil {
			t.Fatal(err)
		}
		inputs[name] = magnet
	}
	bundle, err := release.Verify(filepath.Dir(executable))
	if err != nil {
		t.Fatal(err)
	}
	for name, input := range inputs {
		t.Run(name, func(t *testing.T) {
			e, err := Start(context.Background(), bundle.Engine, t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			defer e.Close()
			ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
			defer cancel()
			if err := e.Admit(ctx, input); err != nil {
				t.Fatal(err)
			}
			var files []File
			for {
				files, err = e.Files(ctx, input.Hash())
				if err != nil {
					t.Fatal(err)
				}
				if len(files) > 0 {
					break
				}
				select {
				case <-ctx.Done():
					t.Fatal("metadata deadline")
				case <-time.After(time.Second):
				}
			}
			// Public multi-file fixtures may list a tiny README before the video.
			file := files[0]
			for _, candidate := range files[1:] {
				if candidate.Length > file.Length {
					file = candidate
				}
			}
			n, err := e.Download(ctx, input.Hash(), file)
			if err != nil || n != min(file.Length, int64(1<<20)) {
				t.Fatal("bounded piece delivery", n, err)
			}
			if err := e.Drop(ctx, input.Hash()); err != nil {
				t.Fatal(err)
			}
			t.Logf("received %d bytes through the authenticated local engine", n)
		})
	}
}
