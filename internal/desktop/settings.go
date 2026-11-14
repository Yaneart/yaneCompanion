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
	mu         sync.RWMutex
	path, site string
}

func OpenSettings(directory string) (*Settings, error) {
	s := &Settings{path: filepath.Join(directory, "desktop.json"), site: SiteOrigin}
	info, err := os.Lstat(s.path)
	if err == nil {
		if !info.Mode().IsRegular() || info.Size() > 4096 {
			return nil, errors.New("SETTINGS_UNAVAILABLE")
		}
		data, err := os.ReadFile(s.path)
		var value struct {
			Site   string `json:"site"`
			Schema int    `json:"schema,omitempty"`
		}
		if err != nil || json.Unmarshal(data, &value) != nil {
			return nil, errors.New("SETTINGS_UNAVAILABLE")
		}
		if value.Schema != 0 && value.Schema != 1 {
			return nil, errors.New("SETTINGS_UNAVAILABLE")
		}
		s.site = value.Site
	} else if !os.IsNotExist(err) {
		return nil, errors.New("SETTINGS_UNAVAILABLE")
	}
	if s.site != "" && !config.ValidOrigin(s.site) {
		return nil, errors.New("INVALID_SITE_ORIGIN")
	}
	return s, nil
}

func (s *Settings) Site() string { s.mu.RLock(); defer s.mu.RUnlock(); return s.site }
func (s *Settings) Allowed(origin string) bool {
	return origin != "" && origin == s.Site() && config.ValidOrigin(origin)
}

func (s *Settings) SetSite(site string) error {
	if len(site) > 512 || !config.ValidOrigin(site) {
		return errors.New("Укажите точный HTTPS-адрес сайта без пути, например https://media.example")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := writePrivateJSON(s.path, struct {
		Site string `json:"site"`
	}{site}); err != nil {
		return err
	}
	s.site = site
	return nil
}
