package app

import (
	"os"
	"path/filepath"
	"testing"

	"yanecompanion/internal/cache"
	"yanecompanion/internal/config"
	"yanecompanion/internal/desktop"
	"yanecompanion/internal/lifecycle"
)

func TestLC011CleanupExplicitChoicesAndLocks(t *testing.T) {
	for _, choice := range []Removal{{}, {Cache: true}, {Pairings: true}, {Cache: true, Pairings: true}} {
		t.Run(string(rune('a'+boolInt(choice.Cache)+2*boolInt(choice.Pairings))), func(t *testing.T) {
			root := t.TempDir()
			paths := config.Paths{Config: filepath.Join(root, "config"), State: filepath.Join(root, "state"), Cache: filepath.Join(root, "cache")}
			if err := paths.Create(); err != nil {
				t.Fatal(err)
			}
			settings, err := desktop.OpenSettings(paths.Config)
			if err != nil {
				t.Fatal(err)
			}
			if err := settings.SetSite("https://watch.example"); err != nil {
				t.Fatal(err)
			}
			settingsPath := filepath.Join(paths.Config, "desktop.json")
			before, _ := os.ReadFile(settingsPath)
			pairings := filepath.Join(paths.Config, "pairings.json")
			os.WriteFile(pairings, []byte("[]"), 0600)
			c, err := cache.Open(filepath.Join(paths.Cache, "sessions"))
			if err != nil {
				t.Fatal(err)
			}
			run, err := c.Create("run-0123456789abcdef0123456789abcdef")
			if err != nil {
				t.Fatal(err)
			}
			c.Close()
			unknown := filepath.Join(paths.Cache, "sessions", "user-file.txt")
			os.WriteFile(unknown, []byte("keep"), 0600)
			updates := filepath.Join(paths.Cache, "updates")
			os.MkdirAll(filepath.Join(updates, "current"), 0700)
			os.WriteFile(filepath.Join(updates, "current", "package"), []byte("cache"), 0600)
			for _, name := range []string{"desktop.lock", "instance.lock"} {
				lock, err := lifecycle.Lock(filepath.Join(paths.State, name))
				if err != nil {
					t.Fatal(err)
				}
				if err := CleanData(paths, choice); err == nil {
					t.Fatal("cleaned a running helper")
				}
				lock.Close()
				if _, err := os.Stat(pairings); err != nil {
					t.Fatal("changed files while locked")
				}
			}
			if err := CleanData(paths, choice); err != nil {
				t.Fatal(err)
			}
			for _, entry := range []struct {
				path    string
				removed bool
			}{{pairings, choice.Pairings}, {run, choice.Cache}, {updates, choice.Cache}} {
				_, err := os.Stat(entry.path)
				if os.IsNotExist(err) != entry.removed {
					t.Fatalf("wrong choice for %s: %v", entry.path, err)
				}
			}
			after, _ := os.ReadFile(settingsPath)
			if string(after) != string(before) {
				t.Fatal("settings changed")
			}
			if _, err := os.Stat(unknown); err != nil {
				t.Fatal("unknown file removed")
			}
			if err := CleanData(paths, choice); err != nil {
				t.Fatal("repeat cleanup", err)
			}
		})
	}
}

func TestLC011CleanupPreservesCheckpointWhenXDGRootsAlias(t *testing.T) {
	for _, allShared := range []bool{false, true} {
		t.Run(map[bool]string{false: "state-cache", true: "config-state-cache"}[allShared], func(t *testing.T) {
			base := t.TempDir()
			shared := filepath.Join(base, "shared")
			paths := config.Paths{Config: filepath.Join(base, "config"), State: shared, Cache: shared}
			if allShared {
				paths.Config = shared
			}
			if err := paths.Create(); err != nil {
				t.Fatal(err)
			}
			updates := filepath.Join(shared, "updates")
			for _, slot := range []string{"current", "previous"} {
				if err := os.MkdirAll(filepath.Join(updates, slot), 0700); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(filepath.Join(updates, slot, "package"), []byte("cache"), 0600); err != nil {
					t.Fatal(err)
				}
			}
			checkpoint := filepath.Join(updates, "checkpoint.json")
			proof := []byte(`{"sequence":99,"digest":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}`)
			if err := os.WriteFile(checkpoint, proof, 0600); err != nil {
				t.Fatal(err)
			}
			unknown := filepath.Join(updates, "user-file")
			os.WriteFile(unknown, []byte("keep"), 0600)
			for i := 0; i < 2; i++ {
				if err := CleanData(paths, Removal{Cache: true}); err != nil {
					t.Fatal(err)
				}
				if data, err := os.ReadFile(checkpoint); err != nil || string(data) != string(proof) {
					t.Fatal("lost anti-replay state", err)
				}
				if _, err := os.Stat(unknown); err != nil {
					t.Fatal("lost unknown update file", err)
				}
				for _, slot := range []string{"current", "previous"} {
					if _, err := os.Stat(filepath.Join(updates, slot)); !os.IsNotExist(err) {
						t.Fatal("owned slot not removed", err)
					}
				}
			}
		})
	}
}

func boolInt(value bool) int {
	if value {
		return 1
	}
	return 0
}

func TestLC011CleanupRejectsUpdateSymlink(t *testing.T) {
	for _, external := range []bool{false, true} {
		t.Run(map[bool]string{false: "inside-cache", true: "outside-cache"}[external], func(t *testing.T) {
			base := t.TempDir()
			paths := config.Paths{Config: filepath.Join(base, "config"), State: filepath.Join(base, "state"), Cache: filepath.Join(base, "cache")}
			if err := paths.Create(); err != nil {
				t.Fatal(err)
			}
			target := filepath.Join(paths.Cache, "user-data")
			if external {
				target = t.TempDir()
			}
			for _, slot := range []string{"current", "previous"} {
				if err := os.MkdirAll(filepath.Join(target, slot), 0700); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(filepath.Join(target, slot, "keep.txt"), []byte("unknown user data"), 0600); err != nil {
					t.Fatal(err)
				}
			}
			linkTarget := target
			if !external {
				linkTarget = "user-data"
			}
			if err := os.Symlink(linkTarget, filepath.Join(paths.Cache, "updates")); err != nil {
				t.Skip("symlink privilege unavailable")
			}
			if err := CleanData(paths, Removal{Cache: true}); err == nil {
				t.Fatal("followed unowned update namespace")
			}
			for _, slot := range []string{"current", "previous"} {
				if data, err := os.ReadFile(filepath.Join(target, slot, "keep.txt")); err != nil || string(data) != "unknown user data" {
					t.Fatal("deleted unowned data", err)
				}
			}
		})
	}
}
