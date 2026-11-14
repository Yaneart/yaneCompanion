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
