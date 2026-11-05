package config

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestUserPaths(t *testing.T) {
	base := filepath.Join(t.TempDir(), "user data \u0418\u043c\u044f")
	for _, key := range []string{"LOCALAPPDATA", "XDG_CONFIG_HOME", "XDG_STATE_HOME", "XDG_CACHE_HOME"} {
		t.Setenv(key, base)
	}
	paths, err := Resolve()
	if err != nil {
		t.Fatal(err)
	}
	if err := paths.Create(); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{paths.Config, paths.State, paths.Cache} {
		if !filepath.IsAbs(path) {
			t.Fatal("relative path")
		}
		info, err := os.Stat(path)
		if err != nil || !info.IsDir() {
			t.Fatal("path unavailable", err)
		}
		if runtime.GOOS == "linux" && info.Mode().Perm() != 0700 {
			t.Fatal("directory is not user-only")
		}
	}
	key := "XDG_STATE_HOME"
	if runtime.GOOS == "windows" {
		key = "LOCALAPPDATA"
	}
	t.Setenv(key, "relative")
	if _, err := Resolve(); err == nil {
		t.Fatal("relative root accepted")
	}
}
