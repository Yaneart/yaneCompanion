package app

import (
	"context"
	"os/exec"
	"time"
)

func openFolder(path string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	return exec.CommandContext(ctx, "xdg-open", path).Run()
}

func openManagement(address string) error {
	cmd := exec.Command("xdg-open", address)
	if err := cmd.Start(); err != nil {
		return err
	}
	go func() { _ = cmd.Wait() }()
	return nil
}
