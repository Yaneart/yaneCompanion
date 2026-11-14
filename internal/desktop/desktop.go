// Package desktop contains the trusted, in-process user interface. Browser
// requests can never invoke its actions or receive its consent snapshots.
package desktop

import "yanecompanion/internal/pairing"

type Snapshot struct {
	Status, Site, Activity string
	Update                 string
	Autostart              bool
	Requests               []pairing.Request
	Grants                 []pairing.Grant
}

type Actions struct {
	Snapshot       func() Snapshot
	SetSite        func(string) error
	Autostart      func(bool) error
	Decide         func(pairing.Request, bool) error
	Revoke         func(string) error
	OpenSite       func() error
	CheckUpdate    func() error
	DownloadUpdate func(bool) error
	UpdateFolder   func() error
	Diagnostics    func() string
	PrepareRemoval func(bool, bool) error
}

func Activity(state string) string {
	switch state {
	case "":
		return "Нет активного просмотра"
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
