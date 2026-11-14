package app

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"

	"yanecompanion/internal/cache"
	"yanecompanion/internal/config"
	"yanecompanion/internal/lifecycle"
)

type Removal struct{ Cache, Pairings bool }

// CleanData is a local, explicit user action; it has no HTTP endpoint. Existing
// desktop and runtime locks must both be free, including a failed-startup GUI.
func CleanData(paths config.Paths, choice Removal) error {
	if err := paths.Create(); err != nil {
		return errors.New("STATE_UNAVAILABLE")
	}
	lock, err := lifecycle.Lock(filepath.Join(paths.State, "desktop.lock"))
	if err != nil {
		return err
	}
	defer lock.Close()
	return cleanStopped(paths, choice)
}

func cleanStopped(paths config.Paths, choice Removal) error {
	lock, err := lifecycle.Lock(filepath.Join(paths.State, "instance.lock"))
	if err != nil {
		return err
	}
	defer lock.Close()
	if choice.Pairings {
		root, err := os.OpenRoot(paths.Config)
		if err != nil {
			return errors.New("STATE_UNAVAILABLE")
		}
		err = root.Remove("pairings.json")
		root.Close()
		if err != nil && !os.IsNotExist(err) {
			return errors.New("PAIRING_STORAGE_UNAVAILABLE")
		}
	}
	if choice.Cache {
		// Cleanup only marked sessions and owned update slots. Unknown files,
		// saved site settings and update replay protection remain intact.
		sessions, err := cache.Open(filepath.Join(paths.Cache, "sessions"))
		if err != nil {
			return errors.New("CACHE_CLEANUP_FAILED")
		}
		err = sessions.Cleanup()
		sessions.Close()
		if err != nil {
			return errors.New("CACHE_CLEANUP_FAILED")
		}
		root, err := os.OpenRoot(paths.Cache)
		if err != nil {
			return errors.New("CACHE_CLEANUP_FAILED")
		}
		err = clearUpdatePackages(root)
		root.Close()
		if err != nil {
			return errors.New("CACHE_CLEANUP_FAILED")
		}
	}
	return nil
}

func clearUpdatePackages(root *os.Root) error {
	info, err := root.Lstat("updates")
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	// Root prevents escapes but can follow a link to unrelated data INSIDE the
	// cache. Such a namespace is not an owned update directory either.
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return errors.New("CACHE_CLEANUP_FAILED")
	}
	updates, err := root.OpenRoot("updates")
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	opened, err := updates.Stat(".")
	if err != nil || !os.SameFile(info, opened) {
		updates.Close()
		return errors.New("CACHE_CLEANUP_FAILED")
	}
	for _, slot := range []string{"current", "previous"} {
		if err := updates.RemoveAll(slot); err != nil {
			updates.Close()
			return err
		}
	}
	// State and cache may resolve to the same XDG directory. Keep the replay
	// checkpoint (and any unknown files) instead of deleting the whole namespace.
	entries, err := fs.ReadDir(updates.FS(), ".")
	updates.Close()
	if err != nil {
		return err
	}
	if len(entries) == 0 {
		return root.Remove("updates")
	}
	return nil
}
