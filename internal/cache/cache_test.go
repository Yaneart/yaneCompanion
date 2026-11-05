package cache

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCrashCleanupOwnsOnlyMarkedRuns(t *testing.T) {
	path := t.TempDir()
	c, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	owned, err := c.Create("run-" + strings.Repeat("a", 32))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(owned, "partial.m4s"), []byte("partial"), 0600); err != nil {
		t.Fatal(err)
	}
	foreign := filepath.Join(path, "run-"+strings.Repeat("b", 32))
	if err := os.Mkdir(foreign, 0700); err != nil {
		t.Fatal(err)
	}
	if err := c.Cleanup(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(owned); !os.IsNotExist(err) {
		t.Fatal("owned crash cache survived")
	}
	if _, err := os.Stat(foreign); err != nil {
		t.Fatal("unmarked data removed")
	}
	if _, err := c.Create("../escape"); err == nil {
		t.Fatal("traversal accepted")
	}
	if err := c.Check(); err != nil {
		t.Fatal(err)
	}
}

func TestCleanupDoesNotFollowSymlink(t *testing.T) {
	path, outside := t.TempDir(), t.TempDir()
	if err := os.WriteFile(filepath.Join(outside, "keep"), []byte("safe"), 0600); err != nil {
		t.Fatal(err)
	}
	name := filepath.Join(path, "run-"+strings.Repeat("c", 32))
	if err := os.Symlink(outside, name); err != nil {
		t.Skip("symlink privilege unavailable")
	}
	c, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	if err := c.Cleanup(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(outside, "keep")); err != nil {
		t.Fatal("external data removed")
	}
	if err := c.Check(); err == nil {
		t.Fatal("cache accepted symlink")
	}
}
