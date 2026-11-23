package desktop

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sync"

	"yanecompanion/internal/config"
)

// SiteOrigin can be set by the owner at release build time. An unconfigured
// development build starts with no public origins, as before.
var SiteOrigin string

type Settings struct {
	mu   sync.RWMutex
	path string
	data desktopSettings
}

type desktopSettings struct {
	Site   *string   `json:"site,omitempty"`
	Theme  ThemeMode `json:"theme,omitempty"`
	Schema int       `json:"schema,omitempty"`
}

func OpenSettings(directory string) (*Settings, error) {
	s := &Settings{path: filepath.Join(directory, "desktop.json")}
	info, err := os.Lstat(s.path)
	if err == nil {
		if !info.Mode().IsRegular() || info.Size() > 4096 {
			return nil, errors.New("SETTINGS_UNAVAILABLE")
		}
		data, err := os.ReadFile(s.path)
		var value desktopSettings
		if err != nil || json.Unmarshal(data, &value) != nil {
			return nil, errors.New("SETTINGS_UNAVAILABLE")
		}
		if value.Schema != 0 && value.Schema != 1 {
			return nil, errors.New("SETTINGS_UNAVAILABLE")
		}
		s.data = value
	} else if !os.IsNotExist(err) {
		return nil, errors.New("SETTINGS_UNAVAILABLE")
	}
	if site := s.Site(); site != "" && !config.ValidOrigin(site) {
		return nil, errors.New("INVALID_SITE_ORIGIN")
	}
	if !s.data.Theme.valid() {
		s.data.Theme = ThemeSystem
	}
	return s, nil
}

func (s *Settings) Site() string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.data.Site == nil {
		return SiteOrigin
	}
	return *s.data.Site
}

func (s *Settings) Theme() ThemeMode { s.mu.RLock(); defer s.mu.RUnlock(); return s.data.Theme }

func (s *Settings) Allowed(origin string) bool {
	return origin != "" && origin == s.Site() && config.ValidOrigin(origin)
}

func (s *Settings) SetSite(site string) error {
	if len(site) > 512 || !config.ValidOrigin(site) {
		return errors.New("Укажите точный HTTPS-адрес сайта без пути, например https://media.example")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	next := s.data
	next.Site = &site
	return s.save(next)
}

func (s *Settings) SetTheme(mode ThemeMode) error {
	if !mode.valid() {
		return errors.New("Неизвестная тема оформления")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.data.Theme == mode {
		return nil
	}
	next := s.data
	next.Theme = mode
	return s.save(next)
}

func (s *Settings) save(next desktopSettings) error {
	if err := writePrivateJSON(s.path, next); err != nil {
		return err
	}
	s.data = next
	return nil
}
