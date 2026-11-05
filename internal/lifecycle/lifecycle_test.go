package lifecycle

import (
	"path/filepath"
	"testing"
)

func TestSingleInstanceLock(t *testing.T) {
	path := filepath.Join(t.TempDir(), "instance.lock")
	first, err := Lock(path)
	if err != nil {
		t.Fatal(err)
	}
	if second, err := Lock(path); err == nil {
		second.Close()
		t.Fatal("second instance acquired lock")
	}
	first.Close()
	third, err := Lock(path)
	if err != nil {
		t.Fatal("lock did not recover after exit:", err)
	}
	third.Close()
}
