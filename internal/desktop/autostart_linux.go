package desktop

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
)

func autostartFile() (string, error) {
	dir, err := os.UserConfigDir()
	if err != nil || !filepath.IsAbs(dir) {
		return "", errors.New("AUTOSTART_UNAVAILABLE")
	}
	return filepath.Join(dir, "autostart", "yanecompanion.desktop"), nil
}

func autostartEntry() (string, error) {
	exe, err := os.Executable()
	if err != nil {
		return "", err
	}
	// Desktop Entry Exec has its own quoting rules, including literal percent.
	quoted := strings.NewReplacer(`\`, `\\`, `"`, `\"`, "`", "\\`", "$", `\$`, "%", "%%").Replace(exe)
	return "[Desktop Entry]\nType=Application\nName=yaneCompanion\nExec=\"" + quoted + "\" desktop --hidden\nIcon=yanecompanion\nTerminal=false\n", nil
}

func AutostartEnabled() bool {
	path, err := autostartFile()
	if err != nil {
		return false
	}
	want, err := autostartEntry()
	if err != nil {
		return false
	}
	data, err := os.ReadFile(path)
	return err == nil && string(data) == want
}

func SetAutostart(enabled bool) error {
	path, err := autostartFile()
	if err != nil {
		return err
	}
	if !enabled {
		err := os.Remove(path)
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	entry, err := autostartEntry()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}
	return os.WriteFile(path, []byte(entry), 0600)
}

func replaceSettings(source, target string) error { return os.Rename(source, target) }
