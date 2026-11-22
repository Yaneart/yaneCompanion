package app

import (
	"os/exec"
	"path/filepath"
	"syscall"

	"golang.org/x/sys/windows"
)

func openFolder(path string) error {
	directory, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return err
	}
	return windows.ShellExecute(0, windows.StringToUTF16Ptr("open"), directory, nil, nil, windows.SW_SHOWNORMAL)
}

func openManagement(address string) error {
	directory, err := windows.GetSystemDirectory()
	if err != nil {
		return err
	}
	cmd := exec.Command(filepath.Join(directory, "rundll32.exe"), "url.dll,FileProtocolHandler", address)
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
	if err := cmd.Start(); err != nil {
		return err
	}
	go func() { _ = cmd.Wait() }()
	return nil
}
