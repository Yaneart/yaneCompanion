package cache

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

func TestCheckDuringMediaEviction(t *testing.T) {
	c, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	run, err := c.Create("run-" + strings.Repeat("d", 32))
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 32; i++ {
		path := filepath.Join(run, fmt.Sprintf("media-%d", i))
		if err := os.Mkdir(path, 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(path, "segment"), []byte("media"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	stop := make(chan struct{})
	var worker sync.WaitGroup
	worker.Add(1)
	go func() {
		defer worker.Done()
		for i := 0; ; i++ {
			select {
			case <-stop:
				return
			default:
			}
			path := filepath.Join(run, fmt.Sprintf("media-%d", i%32))
			_ = os.RemoveAll(path)
			_ = os.Mkdir(path, 0700)
			_ = os.WriteFile(filepath.Join(path, "segment"), []byte("media"), 0600)
		}
	}()
	defer func() { close(stop); worker.Wait() }()
	for i := 0; i < 200; i++ {
		if err := c.Check(); err != nil {
			t.Fatal("normal fragment eviction killed the cache monitor:", err)
		}
	}
}

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
