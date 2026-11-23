// Package desktop contains the trusted, in-process user interface. Browser
// requests can never invoke its actions or receive its consent snapshots.
package desktop

import (
	_ "embed"
	"yanecompanion/internal/pairing"
)

// banner uses the YaneMark from yaneMedia's dark theme.
//
//go:embed banner.png
var banner []byte

const (
	windowTitle    = "yaneCompanion"
	windowSubtitle = "Локальный просмотр yaneMedia"
	windowWidth    = 560
	windowHeight   = 470
	windowCurve    = 24
	headerHeight   = 96
	navX           = 24
	navWidth       = 512
	navHeight      = 44
	panelX         = 24
	panelY         = 153
	panelWidth     = 512
	panelHeight    = 257
)

func (s Snapshot) connectionText() (title, body string) {
	if !s.Ready {
		return "Помощник пока не готов", s.Status
	}
	if s.Activity != "" {
		return "Локальный просмотр", s.Activity
	}
	for _, grant := range s.Grants {
		if grant.Origin == s.ActiveSite() {
			return "Доступ разрешён", "Выберите торрент на сайте, чтобы начать просмотр."
		}
	}
	if s.ActiveSite() == "" {
		return "Укажите адрес сайта", "Затем откройте yaneMedia и нажмите «Подключить»."
	}
	return "Ожидаем подключения", "Нажмите «Подключить» на сайте и подтвердите запрос здесь."
}

func (s Snapshot) ActiveSite() string {
	if s.LaunchOrigin != "" {
		return s.LaunchOrigin
	}
	return s.Site
}

type Snapshot struct {
	Status, Site, Activity string
	Theme                  ThemeMode
	LaunchOrigin           string
	Update                 string
	UpdatesAvailable       bool
	Autostart              bool
	Ready                  bool
	Requests               []pairing.Request
	Grants                 []pairing.Grant
}

type Actions struct {
	Snapshot       func() Snapshot
	SetSite        func(string) error
	SetTheme       func(ThemeMode) error
	Autostart      func(bool) error
	Decide         func(pairing.Request, bool) error
	Revoke         func(string) error
	CheckUpdate    func() error
	DownloadUpdate func(bool) error
	UpdateFolder   func() error
	Diagnostics    func() string
}

func Activity(state string) string {
	switch state {
	case "":
		return ""
	case "ready":
		return "Локальный просмотр активен"
	case "loading", "metadata":
		return "Загружаем сведения о торренте"
	case "preparing", "probing":
		return "Подготавливаем видео"
	case "error", "failed":
		return "Не удалось подготовить видео; подробности на сайте"
	default:
		return "Обрабатываем локальный просмотр"
	}
}
