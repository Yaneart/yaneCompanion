package app

import (
	"context"
	"errors"
	"io"
	"sync"

	"yanecompanion/internal/desktop"
	"yanecompanion/internal/diagnostics"
	"yanecompanion/internal/lifecycle"
	"yanecompanion/internal/pairing"
	"yanecompanion/internal/sessions"
)

func Desktop(ctx context.Context, options Options, hidden bool) error {
	if err := options.Paths.Create(); err != nil {
		return err
	}
	instance, err := desktop.OpenInstance(options.Paths.State)
	if errors.Is(err, lifecycle.ErrRunning) {
		return desktop.Activate(ctx, options.Paths.State)
	}
	if err != nil {
		return err
	}
	defer instance.Close()
	settings, err := desktop.OpenSettings(options.Paths.Config)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	maintenance := newMaintenance(options.Paths)
	defer maintenance.client.Close()
	defer maintenance.wg.Wait()
	defer cancel()
	show := instance.Show
	var mu sync.Mutex
	var store *pairing.Store
	var manager *sessions.Manager
	var startupError error
	var runtimeError error
	var runtimeReady bool
	var removal *Removal
	status := "Запускаем yaneCompanion…"
	options.OriginAllowed = settings.Allowed
	options.Show = func() {
		select {
		case show <- struct{}{}:
		default:
		}
	}
	options.DesktopReady = func(p *pairing.Store, m *sessions.Manager) {
		mu.Lock()
		defer mu.Unlock()
		store, manager, status = p, m, "Готов к подключению"
		runtimeReady = true
	}
	actions := desktop.Actions{
		Snapshot: func() desktop.Snapshot {
			mu.Lock()
			defer mu.Unlock()
			s := desktop.Snapshot{Status: status, Site: settings.Site(), Autostart: desktop.AutostartEnabled()}
			if store != nil {
				s.Requests, s.Grants = store.Snapshot()
			}
			if manager != nil {
				s.Activity = desktop.Activity(manager.Activity())
			}
			s.Update, _ = maintenance.snapshot()
			return s
		},
		SetSite: func(site string) error {
			mu.Lock()
			defer mu.Unlock()
			if err := settings.SetSite(site); err != nil {
				return err
			}
			if store != nil {
				requests, grants := store.Snapshot()
				for _, r := range requests {
					if r.Origin != site {
						if err := store.Decide(r.RequestID, r.Code, false); err != nil {
							return err
						}
					}
				}
				for _, g := range grants {
					if g.Origin != site {
						if err := store.Revoke(g.ID); err != nil {
							return err
						}
					}
				}
			}
			return nil
		},
		Autostart: desktop.SetAutostart,
		Decide: func(r pairing.Request, approve bool) error {
			mu.Lock()
			defer mu.Unlock()
			if store == nil {
				return errors.New("Помощник ещё не готов")
			}
			return store.Decide(r.RequestID, r.Code, approve)
		},
		Revoke: func(id string) error {
			mu.Lock()
			defer mu.Unlock()
			if store == nil {
				return errors.New("Помощник ещё не готов")
			}
			return store.Revoke(id)
		},
		OpenSite: func() error {
			site := settings.Site()
			if site == "" {
				return errors.New("Сначала сохраните адрес сайта")
			}
			return openManagement(site)
		},
		CheckUpdate: func() error { return maintenance.check(ctx) },
		DownloadUpdate: func(previous bool) error {
			mu.Lock()
			defer mu.Unlock()
			return maintenance.download(ctx, previous, manager != nil && manager.Activity() != "")
		},
		UpdateFolder: maintenance.folder,
		Diagnostics: func() string {
			mu.Lock()
			defer mu.Unlock()
			activity, code := "", ""
			if manager != nil {
				activity, code = manager.Activity(), manager.DiagnosticError()
			}
			var playbackError error
			if code != "" {
				playbackError = errors.New(code)
			}
			_, updateCode := maintenance.snapshot()
			return diagnostics.Report(startupError, runtimeError, activity, playbackError, updateCode)
		},
		PrepareRemoval: func(clearCache, clearPairings bool) error {
			mu.Lock()
			defer mu.Unlock()
			if manager != nil && manager.Activity() != "" {
				return errors.New("UPDATE_PLAYBACK_ACTIVE")
			}
			if err := desktop.SetAutostart(false); err != nil {
				return err
			}
			if manager != nil {
				if err := manager.Quiesce(); err != nil {
					return err
				}
			}
			removal = &Removal{Cache: clearCache, Pairings: clearPairings}
			cancel()
			return nil
		},
	}
	done := make(chan error, 1)
	go func() {
		err := Run(ctx, options)
		if errors.Is(err, lifecycle.ErrRunning) {
			_ = Control(ctx, options.Paths, "show", io.Discard)
			cancel()
		}
		mu.Lock()
		store, manager = nil, nil
		if runtimeReady {
			runtimeError = err
		} else {
			startupError = err
		}
		if err != nil {
			status = diagnostics.Message(err) + " (" + diagnostics.Code(err) + ")"
		} else {
			status = "Помощник остановлен"
		}
		mu.Unlock()
		done <- err
		if err == nil {
			cancel()
		}
	}()
	uiErr := desktop.Run(ctx, actions, show, hidden)
	cancel()
	runErr := <-done
	maintenance.wg.Wait()
	if uiErr != nil {
		return uiErr
	}
	if errors.Is(runErr, lifecycle.ErrRunning) {
		return nil
	}
	if removal != nil {
		return cleanStopped(options.Paths, *removal)
	}
	// Runtime errors have already been shown in the persistent desktop status.
	return nil
}
