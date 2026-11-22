package desktop

import (
	"testing"

	"yanecompanion/internal/pairing"
)

func TestConnectionTextUsesActiveSite(t *testing.T) {
	s := Snapshot{Ready: true, LaunchOrigin: "http://localhost:5173", Grants: []pairing.Grant{{Origin: "https://other.example"}}}
	if title, _ := s.connectionText(); s.ActiveSite() != s.LaunchOrigin || title != "Ожидаем подключения" {
		t.Fatalf("launch origin was not respected: %q, %q", s.ActiveSite(), title)
	}
	s.Grants = append(s.Grants, pairing.Grant{Origin: s.LaunchOrigin})
	if title, _ := s.connectionText(); title != "Доступ разрешён" {
		t.Fatalf("current site's grant was not shown: %q", title)
	}
	s.LaunchOrigin = ""
	if title, _ := s.connectionText(); title != "Укажите адрес сайта" {
		t.Fatalf("historical grants should not replace site setup: %q", title)
	}
}
