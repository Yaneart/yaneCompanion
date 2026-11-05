package app

import "os/exec"

func openManagement(address string) error {
	cmd := exec.Command("xdg-open", address)
	if err := cmd.Start(); err != nil {
		return err
	}
	go func() { _ = cmd.Wait() }()
	return nil
}
