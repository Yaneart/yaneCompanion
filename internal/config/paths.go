package config

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
)

const Address = "127.0.0.1:45723"

type Paths struct{ Config, State, Cache string }

func Resolve() (Paths, error) {
	if runtime.GOOS == "windows" {
		base := os.Getenv("LOCALAPPDATA")
		if !filepath.IsAbs(base) {
			return Paths{}, errors.New("INVALID_LOCALAPPDATA")
		}
		base = filepath.Join(base, "yaneCompanion")
		return Paths{base, base, filepath.Join(base, "cache")}, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return Paths{}, err
	}
	resolve := func(key, fallback string) (string, error) {
		base := os.Getenv(key)
		if base == "" {
			base = filepath.Join(home, fallback)
		}
		if !filepath.IsAbs(base) {
			return "", errors.New("INVALID_XDG_PATH")
		}
		return filepath.Join(base, "yanecompanion"), nil
	}
	cfg, err := resolve("XDG_CONFIG_HOME", ".config")
	if err != nil {
		return Paths{}, err
	}
	state, err := resolve("XDG_STATE_HOME", ".local/state")
	if err != nil {
		return Paths{}, err
	}
	cache, err := resolve("XDG_CACHE_HOME", ".cache")
	if err != nil {
		return Paths{}, err
	}
	return Paths{cfg, state, cache}, nil
}

func (p Paths) Create() error {
	for _, path := range []string{p.Config, p.State, p.Cache} {
		if !filepath.IsAbs(path) {
			return errors.New("INVALID_PATH")
		}
		if err := os.MkdirAll(path, 0700); err != nil {
			return err
		}
		info, err := os.Lstat(path)
		if err != nil {
			return err
		}
		if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
			return errors.New("UNSAFE_DIRECTORY")
		}
		if err := secureDirectory(path); err != nil {
			return err
		}
	}
	return nil
}
