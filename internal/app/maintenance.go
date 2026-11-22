package app

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"

	"yanecompanion/internal/config"
	"yanecompanion/internal/diagnostics"
	"yanecompanion/internal/update"
	"yanecompanion/release"
)

type maintenance struct {
	mu            sync.Mutex
	wg            sync.WaitGroup
	client        *update.Client
	busy          bool
	message, code string
}

func newMaintenance(paths config.Paths) *maintenance {
	platform := runtime.GOOS + "-" + runtime.GOARCH
	if runtime.GOOS == "linux" {
		platform = linuxPackagePlatform()
	}
	m := &maintenance{client: update.New(platform, release.Version, paths.State, paths.Cache), message: "Проверьте, доступна ли новая версия.", code: "OK"}
	if !m.configured() {
		m.code = "UPDATE_NOT_CONFIGURED"
		m.message = diagnostics.Message(errors.New(m.code))
	}
	return m
}

func (m *maintenance) configured() bool { return m.client.Feed != "" && m.client.Key != "" }

func linuxPackagePlatform() string {
	data, err := os.ReadFile("/etc/os-release")
	if err != nil || len(data) > 16384 {
		return ""
	}
	for _, line := range strings.Split(string(data), "\n") {
		if !strings.HasPrefix(line, "ID=") {
			continue
		}
		switch strings.Trim(strings.TrimPrefix(line, "ID="), "\"'") {
		case "debian", "ubuntu":
			return "linux-deb-amd64"
		case "arch":
			return "linux-arch-amd64"
		}
	}
	return ""
}

func (m *maintenance) snapshot() (string, string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.message, m.code
}

// Long network operations never block the native event loop or pairing consent.
func (m *maintenance) start(ctx context.Context, action func() (string, error)) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.busy {
		return errors.New("Дождитесь завершения проверки или загрузки")
	}
	if ctx.Err() != nil {
		return ctx.Err()
	}
	m.busy = true
	m.message = "Проверяем или загружаем пакет…"
	m.wg.Add(1)
	go func() {
		defer m.wg.Done()
		message, err := action()
		m.mu.Lock()
		defer m.mu.Unlock()
		m.busy = false
		m.code = diagnostics.Code(err)
		if err != nil {
			m.message = diagnostics.Message(err)
		} else {
			m.message = message
		}
	}()
	return nil
}

func (m *maintenance) check(ctx context.Context) error {
	return m.start(ctx, func() (string, error) {
		manifest, err := m.client.Check(ctx)
		if err != nil {
			return "", err
		}
		order, err := update.Compare(manifest.Current.Version, release.Version)
		if err != nil {
			return "", err
		}
		message := "Доступна версия " + manifest.Current.Version + "."
		if order <= 0 {
			message = "Установлена актуальная версия " + release.Version + "."
		}
		if manifest.Previous != nil {
			message += " Для отката доступна " + manifest.Previous.Version + "."
		}
		return message, nil
	})
}

func (m *maintenance) download(ctx context.Context, previous, active bool) error {
	if active {
		return errors.New("UPDATE_PLAYBACK_ACTIVE")
	}
	return m.start(ctx, func() (string, error) {
		_, err := m.client.Download(ctx, previous, false)
		if err != nil {
			return "", err
		}
		return "Пакет проверен. Откройте папку пакетов, затем выйдите из помощника и установите пакет. Настройки сохраняются.", nil
	})
}

func (m *maintenance) folder() error {
	dir := filepath.Join(m.client.Cache, "updates")
	if err := os.MkdirAll(dir, 0700); err != nil {
		return errors.New("UPDATE_STORAGE_UNAVAILABLE")
	}
	if err := openFolder(dir); err != nil {
		return errors.New("Не удалось открыть папку пакетов.")
	}
	return nil
}
