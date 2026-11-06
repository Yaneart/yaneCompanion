package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"

	"yanecompanion/internal/app"
	"yanecompanion/internal/config"
	"yanecompanion/internal/lifecycle"
	"yanecompanion/release"
)

func main() {
	if err := run(); err != nil {
		var pathError *os.PathError
		if errors.As(err, &pathError) {
			fmt.Fprintln(os.Stderr, "FILESYSTEM_UNAVAILABLE")
		} else {
			fmt.Fprintln(os.Stderr, err)
		}
		os.Exit(1)
	}
}

func run() error {
	command := "run"
	args := os.Args[1:]
	if len(args) > 0 && len(args[0]) > 0 && args[0][0] != '-' {
		command = args[0]
		args = args[1:]
	}
	flags := flag.NewFlagSet("yaneCompanion", flag.ContinueOnError)
	origin := flags.String("origin", "", "explicit HTTPS development origin (no production default)")
	developmentOrigin := flags.String("dev-origin", "", "explicit HTTP localhost/127.0.0.1/[::1] development origin")
	if err := flags.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil
		}
		return err
	}
	if flags.NArg() != 0 {
		return fmt.Errorf("INVALID_COMMAND")
	}
	if *origin != "" && *developmentOrigin != "" {
		return fmt.Errorf("USE_ONE_ORIGIN_FLAG")
	}
	if command == "version" {
		fmt.Println("yaneCompanion", release.Version)
		return nil
	}
	paths, err := config.Resolve()
	if err != nil {
		return err
	}
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	if command == "status" || command == "exit" || command == "manage" {
		return app.Control(ctx, paths, command, os.Stdout)
	}
	if command != "run" {
		return fmt.Errorf("INVALID_COMMAND")
	}
	if err := lifecycle.ContainHost(); err != nil {
		return fmt.Errorf("PROCESS_CONTAINMENT_UNAVAILABLE")
	}
	executable, err := os.Executable()
	if err != nil {
		return fmt.Errorf("BUNDLE_MISSING")
	}
	options := app.Options{Paths: paths, BundleDir: filepath.Dir(executable), Ready: func() { fmt.Println("yaneCompanion ready (local torrent admission available; playback unavailable)") }}
	if *origin != "" {
		options.Origins = []string{*origin}
	}
	options.DevelopmentOrigin = *developmentOrigin
	return app.Run(ctx, options)
}
