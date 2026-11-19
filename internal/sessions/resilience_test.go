package sessions

import (
	"context"
	"errors"
	"testing"
	"time"

	"yanecompanion/internal/engine"
)

type heartbeatFailureEngine struct {
	fakeEngine
	failure error
}

func (e *heartbeatFailureEngine) Files(ctx context.Context, hash string) ([]engine.File, error) {
	if e.failure != nil {
		return nil, e.failure
	}
	return e.fakeEngine.Files(ctx, hash)
}

func TestHeartbeatFailureRetainsTheReasonAfterCleanup(t *testing.T) {
	for _, failure := range []error{engine.ErrNoPeers, engine.ErrUnavailable} {
		t.Run(failure.Error(), func(t *testing.T) {
			e := &heartbeatFailureEngine{}
			m := New(e)
			defer m.Close()
			c, _ := m.CreateController(origin, "grant")
			a, err := m.Create(origin, c.Token, key, []byte("create"), input())
			if err != nil {
				t.Fatal(err)
			}
			waitSnapshot(t, m, c, a.SessionID, "awaiting_file")
			e.failure = failure
			_, err = m.Heartbeat(origin, c.Token)
			code(t, err, failure.Error())
			m.Close()
			if m.DiagnosticError() != failure.Error() {
				t.Fatal("failure disappeared with its session")
			}
		})
	}
}

func TestStoragePressureCancelsWorkAndAllowsExplicitRecovery(t *testing.T) {
	for _, phase := range []string{"metadata", "select", "idle"} {
		t.Run(phase, func(t *testing.T) {
			e := &fakeEngine{}
			if phase == "metadata" {
				e.metadata = make(chan struct{})
			}
			m, c := setup(t, e)
			a, err := m.Create(origin, c.Token, key, []byte("create"), input())
			if err != nil {
				t.Fatal(err)
			}
			operationKey := key
			if phase != "metadata" {
				s := waitSnapshot(t, m, c, a.SessionID, "awaiting_file")
				if phase == "select" {
					e.download = make(chan struct{})
					operationKey = key2
					_, err = m.Select(origin, c.Token, s.ID, key2, []byte("select"), s.Revision, s.Files[0].Ref)
					if err != nil {
						t.Fatal(err)
					}
				}
			}
			m.mu.Lock()
			done := m.active.done
			m.mu.Unlock()
			m.SetStorageAvailable(false)
			select {
			case <-done:
			case <-time.After(2 * time.Second):
				t.Fatal("disk pressure did not join work and Drop")
			}
			s := waitSnapshot(t, m, c, a.SessionID, "failed")
			if s.Error != "CACHE_FULL" || s.Source != nil || m.Activity() != "" || m.DiagnosticError() != "CACHE_FULL" || e.drops.Load() != 1 {
				t.Fatalf("lost failure or leaked device ownership: %+v", s)
			}
			if phase != "idle" {
				op, err := m.Operation(origin, c.Token, operationKey)
				if err != nil || op.State != "failed" || op.Error != "CACHE_FULL" {
					t.Fatalf("operation remained pending: %+v %v", op, err)
				}
			}
			_, err = m.Heartbeat(origin, c.Token)
			code(t, err, "CACHE_FULL")
			_, err = m.Create(origin, c.Token, "123e4567-e89b-42d3-a456-426614174002", []byte("next"), input())
			status, failure := Code(err)
			if status != 507 || failure != "CACHE_FULL" {
				t.Fatal("new work admitted under pressure", status, failure)
			}
			// Clearing pressure neither downloads nor revives the failed session.
			m.SetStorageAvailable(true)
			if m.Activity() != "" || m.DiagnosticError() != "CACHE_FULL" {
				t.Fatal("pressure recovery restarted work or hid the last failure")
			}
			if e.metadata != nil {
				close(e.metadata)
			}
			next, err := m.Create(origin, c.Token, "123e4567-e89b-42d3-a456-426614174002", []byte("next"), input())
			if err != nil {
				t.Fatal(err)
			}
			waitSnapshot(t, m, c, next.SessionID, "awaiting_file")
			if _, err := m.Heartbeat(origin, c.Token); err != nil || m.DiagnosticError() != "" {
				t.Fatal("explicit retry did not recover", err)
			}
		})
	}
}

func TestExpiredLeaseAfterSleepCancelsPendingWork(t *testing.T) {
	e := &fakeEngine{download: make(chan struct{})}
	m, c := setup(t, e)
	a, _ := m.Create(origin, c.Token, key, []byte("create"), input())
	s := waitSnapshot(t, m, c, a.SessionID, "awaiting_file")
	if _, err := m.Select(origin, c.Token, s.ID, key2, []byte("select"), s.Revision, s.Files[0].Ref); err != nil {
		t.Fatal(err)
	}
	m.mu.Lock()
	done := m.active.done
	wake := m.now().Add(181 * time.Second)
	m.now = func() time.Time { return wake }
	m.mu.Unlock()
	// A resumed heartbeat must prune before renewing the old lease.
	_, err := m.Heartbeat(origin, c.Token)
	code(t, err, "CONTROLLER_REQUIRED")
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("expired lease retained a worker")
	}
	if m.Activity() != "" || e.drops.Load() != 1 {
		t.Fatal("expired controller retained device resources")
	}
}

func TestMediaFailureCodesNeverExposePrivateErrors(t *testing.T) {
	for err, want := range map[error]string{
		context.DeadlineExceeded:                               "PREPARATION_TIMEOUT",
		errors.New("RESOURCE_LIMIT"):                           "RESOURCE_LIMIT",
		errors.New("OUTPUT_LIMIT"):                             "RESOURCE_LIMIT",
		errors.New("NO_PEERS"):                                 "NO_PEERS",
		errors.New("http://127.0.0.1/private-token/input.mkv"): "MEDIA_FAILED",
	} {
		if got := mediaFailure(err); got != want {
			t.Fatalf("got %s, want %s", got, want)
		}
	}
}
