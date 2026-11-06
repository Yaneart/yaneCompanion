package sessions

import (
	"context"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"yanecompanion/internal/engine"
	"yanecompanion/internal/locator"
)

type fakeEngine struct {
	adds, drops atomic.Int32
	checks      atomic.Int32
	download    chan struct{}
	metadata    chan struct{}
}

func (e *fakeEngine) Admit(context.Context, locator.Input) error { e.adds.Add(1); return nil }
func (e *fakeEngine) Files(ctx context.Context, _ string) ([]engine.File, error) {
	e.checks.Add(1)
	if e.metadata != nil {
		select {
		case <-e.metadata:
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	return []engine.File{{Index: 1, Path: "pack/one.mkv", Length: 2 << 20}, {Index: 2, Path: "pack/two.mkv", Length: 2 << 20}}, nil
}

func TestHeartbeatKeepsOnlyOwnedIdleMetadataAlive(t *testing.T) {
	e := &fakeEngine{}
	m, c := setup(t, e)
	if _, err := m.Heartbeat(origin, c.Token); err != nil || e.checks.Load() != 0 {
		t.Fatal("idle heartbeat created engine work")
	}
	admission, err := m.Create(origin, c.Token, key, []byte("body"), input())
	if err != nil {
		t.Fatal(err)
	}
	waitSnapshot(t, m, c, admission.SessionID, "awaiting_file")
	before := e.checks.Load()
	if _, err := m.Heartbeat(origin, c.Token); err != nil || e.checks.Load() != before+1 {
		t.Fatal("owned torrent metadata not kept alive")
	}
	other, _ := m.CreateController(origin, "other")
	if _, err := m.Heartbeat(origin, other.Token); err != nil || e.checks.Load() != before+1 {
		t.Fatal("foreign controller touched active engine")
	}
}
func (e *fakeEngine) Download(ctx context.Context, _ string, _ engine.File) (int64, error) {
	if e.download != nil {
		select {
		case <-e.download:
		case <-ctx.Done():
			return 0, ctx.Err()
		}
	}
	return 1 << 20, nil
}
func (e *fakeEngine) Drop(context.Context, string) error { e.drops.Add(1); return nil }

const origin = "https://watch.example"
const key = "123e4567-e89b-42d3-a456-426614174000"
const key2 = "123e4567-e89b-42d3-a456-426614174001"

func input() locator.Locator {
	return locator.Locator{Kind: "magnet", Value: "magnet:?xt=urn:btih:" + strings.Repeat("a", 40)}
}
func setup(t *testing.T, e *fakeEngine) (*Manager, Controller) {
	t.Helper()
	m := New(e)
	c, err := m.CreateController(origin, "grant")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(m.Close)
	return m, c
}
func waitSnapshot(t *testing.T, m *Manager, c Controller, id, state string) Snapshot {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		s, err := m.Snapshot(origin, c.Token, id)
		if err == nil && s.State == state {
			return s
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("snapshot state deadline", state)
	return Snapshot{}
}
func code(t *testing.T, err error, want string) {
	t.Helper()
	if err == nil || err.Error() != want {
		t.Fatalf("error %v, want %s", err, want)
	}
}

func TestOwnershipReplayAndFileSelection(t *testing.T) {
	e := &fakeEngine{download: make(chan struct{})}
	m, c := setup(t, e)
	admission, err := m.Create(origin, c.Token, key, []byte("body"), input())
	if err != nil {
		t.Fatal(err)
	}
	s := waitSnapshot(t, m, c, admission.SessionID, "awaiting_file")
	if len(s.Files) != 2 {
		t.Fatal("missing multi-file metadata")
	}
	replay, err := m.Create(origin, c.Token, key, []byte("body"), input())
	if err != nil || replay != admission || e.adds.Load() != 1 {
		t.Fatal("create replay ran work twice")
	}
	_, err = m.Create(origin, c.Token, key, []byte("different"), input())
	code(t, err, "IDEMPOTENCY_CONFLICT")
	other, _ := m.CreateController(origin, "other")
	_, err = m.Snapshot(origin, other.Token, s.ID)
	code(t, err, "NOT_FOUND")
	_, err = m.Create(origin, other.Token, key, []byte("body"), input())
	code(t, err, "DEVICE_BUSY")
	_, err = m.Snapshot("https://evil.example", c.Token, s.ID)
	code(t, err, "CONTROLLER_REQUIRED")
	selection, err := m.Select(origin, c.Token, s.ID, key2, []byte("select"), s.Revision, s.Files[1].Ref)
	if err != nil {
		t.Fatal(err)
	}
	repeated, err := m.Select(origin, c.Token, s.ID, key2, []byte("select"), s.Revision, s.Files[1].Ref)
	if err != nil || repeated != selection {
		t.Fatal("selection replay did not precede CAS")
	}
	close(e.download)
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		op, _ := m.Operation(origin, c.Token, key2)
		if op.State == "ready" {
			break
		}
		time.Sleep(time.Millisecond)
	}
	latest, _ := m.Snapshot(origin, c.Token, s.ID)
	if latest.Downloaded != 1<<20 || latest.Selected != s.Files[1].Ref {
		t.Fatal("wrong selected file or byte budget")
	}
	if err := m.Stop(origin, c.Token, s.ID); err != nil {
		t.Fatal(err)
	}
	waitSnapshot(t, m, c, s.ID, "stopped")
	if e.drops.Load() != 1 {
		t.Fatal("engine not dropped")
	}
}
func TestStopRevisionOneAndRevokePreventLatePublication(t *testing.T) {
	e := &fakeEngine{metadata: make(chan struct{})}
	m, c := setup(t, e)
	admission, err := m.Create(origin, c.Token, key, []byte("body"), input())
	if err != nil {
		t.Fatal(err)
	}
	if err := m.Cancel(origin, c.Token, key, 1); err != nil {
		t.Fatal(err)
	}
	close(e.metadata)
	s := waitSnapshot(t, m, c, admission.SessionID, "stopped")
	if len(s.Files) != 0 {
		t.Fatal("cancelled metadata published")
	}
	op, _ := m.Operation(origin, c.Token, key)
	if op.State != "cancelled" {
		t.Fatal("operation not cancelled")
	}
	admission, err = m.Create(origin, c.Token, key2, []byte("new"), input())
	if err != nil {
		t.Fatal(err)
	}
	waitSnapshot(t, m, c, admission.SessionID, "awaiting_file")
	m.Revoke("grant")
	_, err = m.Snapshot(origin, c.Token, admission.SessionID)
	code(t, err, "CONTROLLER_REQUIRED")
}
func TestLeaseAndExpiredOperationTombstone(t *testing.T) {
	m, c := setup(t, &fakeEngine{})
	admission, _ := m.Create(origin, c.Token, key, []byte("body"), input())
	waitSnapshot(t, m, c, admission.SessionID, "awaiting_file")
	m.mu.Lock()
	now := m.now().Add(11 * time.Minute)
	m.now = func() time.Time { return now }
	for _, controller := range m.controllers {
		controller.expires = now.Add(time.Hour)
	}
	m.mu.Unlock()
	_, err := m.Create(origin, c.Token, key, []byte("body"), input())
	code(t, err, "OPERATION_EXPIRED")
	m.mu.Lock()
	now = now.Add(2 * time.Hour)
	m.mu.Unlock()
	m.Tick()
	_, err = m.Heartbeat(origin, c.Token)
	code(t, err, "CONTROLLER_REQUIRED")
}
