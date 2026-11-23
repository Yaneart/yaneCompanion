package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"runtime"
	"syscall"

	"yanecompanion/internal/app"
	"yanecompanion/internal/config"
	"yanecompanion/internal/desktop"
	"yanecompanion/internal/lifecycle"
	"yanecompanion/release"
)

func main() {
	if err := run(); err != nil {
		if len(os.Args) == 1 || os.Args[1] == "desktop" || isOpenURI(os.Args[1]) {
			desktop.ReportError(err)
		}
		var pathError *os.PathError
		if errors.As(err, &pathError) {
			fmt.Fprintln(os.Stderr, "FILESYSTEM_UNAVAILABLE")
		} else {
			fmt.Fprintln(os.Stderr, err)
		}
		os.Exit(1)
	}
}

func isOpenURI(arg string) bool {
	// Windows ShellExecute can add the root path to the protocol URL.
	return arg == "yanecompanion://open" || arg == "yanecompanion://open/"
}

func run() error {
	command := "desktop"
	args := os.Args[1:]
	if len(args) == 1 && isOpenURI(args[0]) {
		args = nil
	}
	if len(args) > 0 && len(args[0]) > 0 && args[0][0] != '-' {
		command = args[0]
		args = args[1:]
	}
	flags := flag.NewFlagSet("yaneCompanion", flag.ContinueOnError)
	origin := flags.String("origin", "", "explicit HTTPS development origin (no production default)")
	developmentOrigin := flags.String("dev-origin", "", "explicit HTTP localhost/127.0.0.1/[::1] development origin")
	hidden := flags.Bool("hidden", false, "start desktop window hidden (explicit autostart)")
	clearCache := flags.Bool("cache", false, "explicitly remove owned session/update cache with clean-data")
	clearPairings := flags.Bool("pairings", false, "explicitly remove saved pairings with clean-data")
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
	if command == "clean-data" {
		if (!*clearCache && !*clearPairings) || *origin != "" || *developmentOrigin != "" || *hidden {
			return fmt.Errorf("INVALID_COMMAND")
		}
		return app.CleanData(paths, app.Removal{Cache: *clearCache, Pairings: *clearPairings})
	}
	if *clearCache || *clearPairings {
		return fmt.Errorf("INVALID_COMMAND")
	}
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	if command == "status" || command == "exit" || command == "manage" {
		return app.Control(ctx, paths, command, os.Stdout)
	}
	if command != "run" && command != "desktop" {
		return fmt.Errorf("INVALID_COMMAND")
	}
	if runtime.GOOS == "linux" && os.Geteuid() == 0 {
		return fmt.Errorf("RUN_AS_DESKTOP_USER")
	}
	if err := lifecycle.ContainHost(); err != nil {
		return fmt.Errorf("PROCESS_CONTAINMENT_UNAVAILABLE")
	}
	executable, err := os.Executable()
	if err != nil {
		return fmt.Errorf("BUNDLE_MISSING")
	}
	options := app.Options{Paths: paths, BundleDir: filepath.Dir(executable), Ready: func() { fmt.Println("yaneCompanion ready (local torrent admission and media preparation available)") }}
	if *origin != "" {
		options.Origins = []string{*origin}
	}
	options.DevelopmentOrigin = *developmentOrigin
	if command == "desktop" {
		return app.Desktop(ctx, options, *hidden)
	}
	return app.Run(ctx, options)
}
