package desktop

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestAutostartIsExplicitAndUserScoped(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "profile with spaces Проверка")
	t.Setenv("XDG_CONFIG_HOME", dir)
	if AutostartEnabled() {
		t.Fatal("autostart enabled by default")
	}
	if err := SetAutostart(true); err != nil {
		t.Fatal(err)
	}
	if !AutostartEnabled() {
		t.Fatal("autostart did not persist")
	}
	data, err := os.ReadFile(filepath.Join(dir, "autostart", "yanecompanion.desktop"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "desktop --hidden") || strings.Contains(string(data), "sudo") {
		t.Fatal("invalid desktop launch")
	}
	if err := SetAutostart(false); err != nil {
		t.Fatal(err)
	}
	if AutostartEnabled() {
		t.Fatal("autostart was not removed")
	}
	if err := SetAutostart(false); err != nil {
		t.Fatal(err)
	}
}
