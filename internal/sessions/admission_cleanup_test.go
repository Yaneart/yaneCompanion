package sessions

import (
	"context"
	"sync"
	"testing"
	"time"
)

type cleanupAdmissionEngine struct {
	fakeEngine
	gate    chan struct{}
	started chan struct{}
}

func (e *cleanupAdmissionEngine) Drop(ctx context.Context, _ string) error {
	e.drops.Add(1)
	select {
	case e.started <- struct{}{}:
	default:
	}
	select {
	case <-e.gate:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func cleanupAdmissionFixture(t *testing.T) (*Manager, *cleanupAdmissionEngine, Controller, func()) {
	t.Helper()
	e := &cleanupAdmissionEngine{gate: make(chan struct{}), started: make(chan struct{}, 1)}
	m := New(e)
	var once sync.Once
	release := func() { once.Do(func() { close(e.gate) }) }
	t.Cleanup(func() { release(); m.Close() })
	previous, err := m.CreateController(origin, "previous")
	if err != nil {
		t.Fatal(err)
	}
	a, err := m.Create(origin, previous.Token, key, []byte("first"), input())
	if err != nil {
		t.Fatal(err)
	}
	waitSnapshot(t, m, previous, a.SessionID, "awaiting_file")
	if err = m.DeleteController(origin, previous.Token); err != nil {
		t.Fatal(err)
	}
	select {
	case <-e.started:
	case <-time.After(time.Second):
		t.Fatal("drop did not enter")
	}
	next, err := m.CreateController(origin, "next")
	if err != nil {
		t.Fatal(err)
	}
	return m, e, next, release
}

type cleanupAdmissionResult struct {
	admission Admission
	err       error
}

func beginCleanupAdmission(m *Manager, c Controller) <-chan cleanupAdmissionResult {
	result := make(chan cleanupAdmissionResult, 1)
	go func() {
		a, err := m.Create(origin, c.Token, key2, []byte("next"), input())
		result <- cleanupAdmissionResult{a, err}
	}()
	return result
}

func TestNewAdmissionWaitsForAcknowledgedControllerCleanup(t *testing.T) {
	m, e, c, release := cleanupAdmissionFixture(t)
	result := beginCleanupAdmission(m, c)
	select {
	case r := <-result:
		t.Fatalf("admission resolved before old Drop finished: %v", r.err)
	case <-time.After(100 * time.Millisecond):
	}
	if e.adds.Load() != 1 {
		t.Fatal("new torrent admitted during previous cleanup")
	}
	release()
	var next cleanupAdmissionResult
	select {
	case next = <-result:
	case <-time.After(time.Second):
		t.Fatal("admission did not resume after cleanup")
	}
	if next.err != nil {
		t.Fatal(next.err)
	}
	waitSnapshot(t, m, c, next.admission.SessionID, "awaiting_file")
	replay, err := m.Create(origin, c.Token, key2, []byte("next"), input())
	if err != nil || replay != next.admission || e.adds.Load() != 2 {
		t.Fatal("cleanup retry duplicated admission", err)
	}
}

func TestActiveViewerAdmissionRemainsImmediatelyBusy(t *testing.T) {
	e := &fakeEngine{}
	m, c := setup(t, e)
	a, err := m.Create(origin, c.Token, key, []byte("active"), input())
	if err != nil {
		t.Fatal(err)
	}
	waitSnapshot(t, m, c, a.SessionID, "awaiting_file")
	other, err := m.CreateController(origin, "other")
	if err != nil {
		t.Fatal(err)
	}
	result := beginCleanupAdmission(m, other)
	select {
	case r := <-result:
		code(t, r.err, "DEVICE_BUSY")
	case <-time.After(150 * time.Millisecond):
		t.Fatal("active viewer admission waited instead of returning busy")
	}
	if e.adds.Load() != 1 || e.drops.Load() != 0 {
		t.Fatal("active viewer was replaced")
	}
}

func TestCleanupAdmissionRechecksRevokedController(t *testing.T) {
	for _, remove := range []string{"delete", "revoke"} {
		t.Run(remove, func(t *testing.T) {
			m, e, c, release := cleanupAdmissionFixture(t)
			result := beginCleanupAdmission(m, c)
			select {
			case r := <-result:
				t.Fatalf("admission resolved before cleanup: %v", r.err)
			case <-time.After(100 * time.Millisecond):
			}
			if remove == "delete" {
				if err := m.DeleteController(origin, c.Token); err != nil {
					t.Fatal(err)
				}
			} else {
				m.Revoke("next")
			}
			release()
			select {
			case r := <-result:
				code(t, r.err, "CONTROLLER_REQUIRED")
			case <-time.After(time.Second):
				t.Fatal("revoked waiter did not settle")
			}
			if e.adds.Load() != 1 {
				t.Fatal("revoked waiter admitted a new torrent")
			}
		})
	}
}

func TestCleanupAdmissionTimeoutKeepsDeviceOwnership(t *testing.T) {
	m, e, c, _ := cleanupAdmissionFixture(t)
	started := time.Now()
	result := beginCleanupAdmission(m, c)
	select {
	case r := <-result:
		code(t, r.err, "DEVICE_BUSY")
		if elapsed := time.Since(started); elapsed < 1800*time.Millisecond || elapsed > 2800*time.Millisecond {
			t.Fatalf("cleanup wait exceeded its bounded response window: %v", elapsed)
		}
	case <-time.After(2800 * time.Millisecond):
		t.Fatal("cleanup wait exceeded its response budget")
	}
	if e.adds.Load() != 1 || m.Activity() != "stopping" {
		t.Fatal("timeout released old ownership or admitted a replacement")
	}
}

func TestConcurrentCleanupAdmissionReplaysOnce(t *testing.T) {
	m, e, c, release := cleanupAdmissionFixture(t)
	first, second := beginCleanupAdmission(m, c), beginCleanupAdmission(m, c)
	select {
	case r := <-first:
		t.Fatalf("first request escaped cleanup wait: %v", r.err)
	case r := <-second:
		t.Fatalf("second request escaped cleanup wait: %v", r.err)
	case <-time.After(100 * time.Millisecond):
	}
	release()
	var admission Admission
	for _, result := range []<-chan cleanupAdmissionResult{first, second} {
		select {
		case r := <-result:
			if r.err != nil {
				t.Fatal(r.err)
			}
			if admission.SessionID != "" && admission != r.admission {
				t.Fatal("same key created different admissions")
			}
			admission = r.admission
		case <-time.After(time.Second):
			t.Fatal("concurrent replay did not settle")
		}
	}
	waitSnapshot(t, m, c, admission.SessionID, "awaiting_file")
	if e.adds.Load() != 2 {
		t.Fatal("concurrent cleanup wait duplicated the next engine admission")
	}
}
