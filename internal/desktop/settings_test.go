package desktop

import (
	"os"
	"path/filepath"
	"testing"
)

func TestSettingsExactOriginAndPersistence(t *testing.T) {
	directory := t.TempDir()
	s, err := OpenSettings(directory)
	if err != nil {
		t.Fatal(err)
	}
	if s.Allowed("https://media.example") {
		t.Fatal("development build allowed an unconfigured site")
	}
	for _, site := range []string{"http://media.example", "https://*.example", "https://media.example/", "https://media.example/path", "https://user@media.example", "https://media.example?x", "https://media.example#x", "javascript:alert(1)"} {
		if err := s.SetSite(site); err == nil {
			t.Fatalf("accepted %q", site)
		}
	}
	if err := s.SetSite("https://media.example:8443"); err != nil {
		t.Fatal(err)
	}
	for _, origin := range []string{"https://media.example", "https://media.example:8444", "https://other.example", "null", ""} {
		if s.Allowed(origin) {
			t.Fatalf("allowed %q", origin)
		}
	}
	s, err = OpenSettings(directory)
	if err != nil {
		t.Fatal(err)
	}
	if !s.Allowed("https://media.example:8443") {
		t.Fatal("saved exact origin was lost")
	}
	if err := s.SetSite("https://new.example"); err != nil {
		t.Fatal(err)
	}
	if s.Allowed("https://media.example:8443") || !s.Allowed("https://new.example") {
		t.Fatal("old origin remained enabled")
	}
	if err := s.SetSite("http://new.example"); err == nil || s.Site() != "https://new.example" {
		t.Fatal("invalid write changed settings")
	}
}

func TestSettingsThemePersistence(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "desktop.json")
	if err := os.WriteFile(path, []byte(`{"site":"https://media.example"}`), 0600); err != nil {
		t.Fatal(err)
	}
	settings, err := OpenSettings(dir)
	if err != nil || settings.Theme() != ThemeSystem {
		t.Fatalf("legacy settings should use the system theme: %v", err)
	}
	if err := settings.SetTheme(ThemeLight); err != nil {
		t.Fatal(err)
	}
	settings, err = OpenSettings(dir)
	if err != nil || settings.Theme() != ThemeLight || settings.Site() != "https://media.example" {
		t.Fatalf("theme or site lost across reopen: %v", err)
	}
	if err := settings.SetSite("https://new.example"); err != nil {
		t.Fatal(err)
	}
	if err := settings.SetTheme("unknown"); err == nil {
		t.Fatal("invalid theme accepted")
	}
	settings, err = OpenSettings(dir)
	if err != nil || settings.Theme() != ThemeLight || settings.Site() != "https://new.example" {
		t.Fatalf("site update changed the saved theme: %v", err)
	}
	for _, test := range []struct {
		mode        ThemeMode
		light, dark bool
	}{
		{ThemeSystem, false, true},
		{ThemeLight, false, false},
		{ThemeDark, true, true},
	} {
		if test.mode.dark(false) != test.light || test.mode.dark(true) != test.dark {
			t.Errorf("wrong appearance for %s", test.mode)
		}
	}
}

func TestSettingsRejectCorruptOrUnsafeFiles(t *testing.T) {
	for _, data := range []string{"{", `{"site":"http://media.example"}`, `{"site":"https://watch.example","schema":2}`, string(make([]byte, 4097))} {
		dir := t.TempDir()
		if err := os.WriteFile(filepath.Join(dir, "desktop.json"), []byte(data), 0600); err != nil {
			t.Fatal(err)
		}
		if _, err := OpenSettings(dir); err == nil {
			t.Fatal("accepted corrupt settings")
		}
	}
	dir := t.TempDir()
	if err := os.Mkdir(filepath.Join(dir, "desktop.json"), 0700); err != nil {
		t.Fatal(err)
	}
	if _, err := OpenSettings(dir); err == nil {
		t.Fatal("accepted directory as settings")
	}
}
