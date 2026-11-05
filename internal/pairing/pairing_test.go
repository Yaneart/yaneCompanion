package pairing

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

const origin = "https://watch.example"

func store(t *testing.T) *Store {
	t.Helper()
	s, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func approve(t *testing.T, s *Store) (Request, Result) {
	t.Helper()
	r, err := s.Create(origin)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Decide(r.RequestID, r.Code, true); err != nil {
		t.Fatal(err)
	}
	result, err := s.Poll(origin, r.RequestID, r.PollToken)
	if err != nil || result.State != "approved" || len(result.PairingToken) != 64 {
		t.Fatalf("approval: %+v %v", result, err)
	}
	return r, result
}

func TestPersistentConsentAndRevoke(t *testing.T) {
	s := store(t)
	r, result := approve(t, s)
	repeated, err := s.Poll(origin, r.RequestID, r.PollToken)
	if err != nil || repeated != result {
		t.Fatal("approved poll must be repeatable")
	}
	data, err := os.ReadFile(filepath.Join(s.directory, "pairings.json"))
	if err != nil || bytes.Contains(data, []byte(result.PairingToken)) || bytes.Contains(data, []byte(r.PollToken)) {
		t.Fatal("plain bearer persisted")
	}
	requests, grants := s.Snapshot()
	if len(requests) != 0 || len(grants) != 1 {
		t.Fatal("bad management snapshot")
	}
	loaded, err := Open(s.directory)
	if err != nil {
		t.Fatal(err)
	}
	grant, err := loaded.Authenticate(origin, result.PairingToken)
	if err != nil {
		t.Fatal("pairing did not survive boot")
	}
	for _, tc := range []struct{ origin, token string }{{"https://other.example", result.PairingToken}, {origin, r.PollToken}, {origin, ""}, {origin, strings.Repeat("0", 64)}} {
		if _, err := loaded.Authenticate(tc.origin, tc.token); !errors.Is(err, ErrRequired) {
			t.Fatal("foreign or absent bearer accepted")
		}
	}
	if err := loaded.Revoke(grant.ID); err != nil {
		t.Fatal(err)
	}
	loaded, err = Open(s.directory)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := loaded.Authenticate(origin, result.PairingToken); !errors.Is(err, ErrRequired) {
		t.Fatal("revocation did not survive boot")
	}
}

func TestCancellationBeforeAndAfterApproval(t *testing.T) {
	for _, approved := range []bool{false, true} {
		s := store(t)
		r, err := s.Create(origin)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := s.Poll("https://other.example", r.RequestID, r.PollToken); !errors.Is(err, ErrRequest) {
			t.Fatal("cross-origin poll accepted")
		}
		if err := s.Cancel(origin, r.RequestID, strings.Repeat("0", 64)); !errors.Is(err, ErrRequest) {
			t.Fatal("wrong poll proof accepted")
		}
		var token string
		if approved {
			if err := s.Decide(r.RequestID, r.Code, true); err != nil {
				t.Fatal(err)
			}
			result, _ := s.Poll(origin, r.RequestID, r.PollToken)
			token = result.PairingToken
		}
		if err := s.Cancel(origin, r.RequestID, r.PollToken); err != nil {
			t.Fatal(err)
		}
		if err := s.Cancel(origin, r.RequestID, r.PollToken); err != nil {
			t.Fatal("cancel not idempotent")
		}
		result, err := s.Poll(origin, r.RequestID, r.PollToken)
		if err != nil || result.State != "cancelled" || result.PairingToken != "" {
			t.Fatal("cancel did not close proof")
		}
		if err := s.Decide(r.RequestID, r.Code, true); !errors.Is(err, ErrExpired) {
			t.Fatal("cancelled request approved")
		}
		if token != "" {
			if _, err := s.Authenticate(origin, token); !errors.Is(err, ErrRequired) {
				t.Fatal("cancelled approval still usable")
			}
		}
	}
}

func TestExpiryLocalCodeAndRateLimits(t *testing.T) {
	s := store(t)
	now := time.Date(2026, 11, 5, 12, 0, 0, 0, time.UTC)
	s.now = func() time.Time { return now }
	r, _ := s.Create(origin)
	if _, err := s.Create(origin); !errors.Is(err, ErrBusy) {
		t.Fatal("parallel pending request accepted")
	}
	if err := s.Decide(r.RequestID, "wrong", true); !errors.Is(err, ErrExpired) {
		t.Fatal("wrong local code accepted")
	}
	now = now.Add(120 * time.Second)
	result, err := s.Poll(origin, r.RequestID, r.PollToken)
	if err != nil || result.State != "expired" {
		t.Fatal("expired proof remained pending")
	}
	if err := s.Decide(r.RequestID, r.Code, true); !errors.Is(err, ErrExpired) {
		t.Fatal("expired local decision accepted")
	}
	for i := 0; i < 5; i++ {
		r, err := s.Create(origin)
		if err != nil {
			t.Fatal(err)
		}
		if err := s.Decide(r.RequestID, r.Code, false); err != nil {
			t.Fatal(err)
		}
		result, _ := s.Poll(origin, r.RequestID, r.PollToken)
		if result.State != "denied" {
			t.Fatal("denial lost")
		}
	}
	if _, err := s.Create(origin); !errors.Is(err, ErrRate) {
		t.Fatal("rate limit not enforced")
	}
	now = now.Add(time.Minute)
	if _, err := s.Create(origin); err != nil {
		t.Fatal("rate limit failed to expire")
	}
}

func TestRevokedGrantCannotBeRecoveredByPoll(t *testing.T) {
	s := store(t)
	r, result := approve(t, s)
	grant, _ := s.Authenticate(origin, result.PairingToken)
	if err := s.Revoke(grant.ID); err != nil {
		t.Fatal(err)
	}
	polled, err := s.Poll(origin, r.RequestID, r.PollToken)
	if err != nil || polled.PairingToken != "" || polled.State != "denied" {
		t.Fatal("poll recovered revoked token")
	}
}

func TestInvalidStorageFailsClosed(t *testing.T) {
	s := store(t)
	path := filepath.Join(s.directory, "pairings.json")
	for _, data := range []string{"broken", "[{}]", strings.Repeat("x", 33<<10)} {
		if err := os.WriteFile(path, []byte(data), 0600); err != nil {
			t.Fatal(err)
		}
		if _, err := Open(s.directory); !errors.Is(err, ErrStorage) {
			t.Fatal("invalid consent store accepted")
		}
	}
}
