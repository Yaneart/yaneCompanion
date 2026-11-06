// Package pairing owns local consent and persistent, origin-bound token verifiers.
package pairing

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"os"
	"path/filepath"
	"sync"
	"time"
)

var (
	ErrRequired = errors.New("PAIRING_REQUIRED")
	ErrRequest  = errors.New("PAIRING_REQUEST_REQUIRED")
	ErrBusy     = errors.New("PAIRING_PENDING")
	ErrRate     = errors.New("RATE_LIMITED")
	ErrExpired  = errors.New("PAIRING_EXPIRED")
	ErrStorage  = errors.New("PAIRING_STORAGE_UNAVAILABLE")
)

const lifetime = 120 * time.Second

type Request struct {
	RequestID string    `json:"requestId"`
	PollToken string    `json:"pollToken,omitempty"`
	Code      string    `json:"code"`
	Origin    string    `json:"origin,omitempty"`
	ExpiresAt time.Time `json:"expiresAt"`
}

type Result struct {
	State        string `json:"state"`
	PairingToken string `json:"pairingToken,omitempty"`
}

type Grant struct {
	ID        string    `json:"id"`
	Origin    string    `json:"origin"`
	CreatedAt time.Time `json:"createdAt"`
}

type verifier struct {
	Grant
	Digest string `json:"verifier"`
}

type pending struct {
	Request
	proof                 [32]byte
	state, token, grantID string
}

type Store struct {
	// Set once before serving. The callback must not call back into this store.
	OnRevoke  func(string)
	mu        sync.Mutex
	directory string
	grants    []verifier
	requests  map[string]*pending
	starts    []time.Time
	now       func() time.Time
}

func Open(directory string) (*Store, error) {
	s := &Store{directory: directory, requests: make(map[string]*pending), now: time.Now}
	path := filepath.Join(directory, "pairings.json")
	info, err := os.Lstat(path)
	if os.IsNotExist(err) {
		return s, nil
	}
	if err != nil || !info.Mode().IsRegular() || info.Size() > 32<<10 {
		return nil, ErrStorage
	}
	data, err := os.ReadFile(path)
	if err != nil || json.Unmarshal(data, &s.grants) != nil || len(s.grants) > 32 {
		return nil, ErrStorage
	}
	for _, g := range s.grants {
		if !validHex(g.ID, 16) || !validHex(g.Digest, 32) || g.Origin == "" {
			return nil, ErrStorage
		}
	}
	return s, nil
}

func secret(size int) (string, error) {
	b := make([]byte, size)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

func validHex(value string, size int) bool {
	b, err := hex.DecodeString(value)
	return err == nil && len(b) == size && hex.EncodeToString(b) == value
}

func (s *Store) prune() {
	now := s.now()
	for id, p := range s.requests {
		if !now.Before(p.ExpiresAt) {
			p.state, p.token = "expired", ""
			if !now.Before(p.ExpiresAt.Add(lifetime)) {
				delete(s.requests, id)
			}
		}
	}
	kept := s.starts[:0]
	for _, t := range s.starts {
		if now.Sub(t) < time.Minute {
			kept = append(kept, t)
		}
	}
	s.starts = kept
}

func (s *Store) Create(origin string) (Request, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.prune()
	for _, p := range s.requests {
		if p.state == "pending" {
			return Request{}, ErrBusy
		}
	}
	if len(s.starts) >= 5 {
		return Request{}, ErrRate
	}
	id, err := secret(16)
	if err != nil {
		return Request{}, err
	}
	proof, err := secret(32)
	if err != nil {
		return Request{}, err
	}
	code, err := rand.Int(rand.Reader, big.NewInt(1000000))
	if err != nil {
		return Request{}, err
	}
	r := Request{RequestID: id, PollToken: proof, Code: fmt.Sprintf("%06d", code.Int64()), Origin: origin, ExpiresAt: s.now().Add(lifetime)}
	p := &pending{Request: r, proof: sha256.Sum256([]byte(proof)), state: "pending"}
	p.PollToken = ""
	s.requests[id] = p
	s.starts = append(s.starts, s.now())
	return r, nil
}

func (s *Store) Poll(origin, id, token string) (Result, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.prune()
	p, ok := s.requests[id]
	digest := sha256.Sum256([]byte(token))
	if !ok || !validHex(token, 32) || p.Origin != origin || subtle.ConstantTimeCompare(digest[:], p.proof[:]) != 1 {
		return Result{}, ErrRequest
	}
	if p.state == "approved" && !s.hasGrant(p.grantID) {
		p.state, p.token = "denied", ""
	}
	return Result{p.state, p.token}, nil
}

func (s *Store) Cancel(origin, id, token string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.prune()
	p, ok := s.requests[id]
	digest := sha256.Sum256([]byte(token))
	if !ok || !validHex(token, 32) || p.Origin != origin || subtle.ConstantTimeCompare(digest[:], p.proof[:]) != 1 {
		return ErrRequest
	}
	if p.grantID != "" {
		if err := s.revoke(p.grantID); err != nil {
			return err
		}
	}
	p.state, p.token = "cancelled", ""
	return nil
}

func (s *Store) Decide(id, code string, approve bool) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.prune()
	p, ok := s.requests[id]
	if !ok || p.state != "pending" || code != p.Code {
		return ErrExpired
	}
	if !approve {
		p.state = "denied"
		return nil
	}
	if len(s.grants) >= 32 {
		return errors.New("PAIRING_LIMIT")
	}
	token, err := secret(32)
	if err != nil {
		return err
	}
	id, err = secret(16)
	if err != nil {
		return err
	}
	digest := sha256.Sum256([]byte(token))
	g := verifier{Grant: Grant{id, p.Origin, s.now()}, Digest: hex.EncodeToString(digest[:])}
	grants := append(append([]verifier(nil), s.grants...), g)
	if err := s.persist(grants); err != nil {
		return err
	}
	s.grants = grants
	p.state, p.token, p.grantID = "approved", token, id
	return nil
}

func (s *Store) Authenticate(origin, token string) (Grant, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.authenticate(origin, token)
}

// Serialize controller creation with revoke, including an approved-poll cancel.
func (s *Store) UseGrant(origin, token string, use func(Grant) error) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	grant, err := s.authenticate(origin, token)
	if err != nil {
		return err
	}
	return use(grant)
}

func (s *Store) authenticate(origin, token string) (Grant, error) {
	if !validHex(token, 32) {
		return Grant{}, ErrRequired
	}
	digest := sha256.Sum256([]byte(token))
	for _, g := range s.grants {
		if g.Origin == origin && subtle.ConstantTimeCompare([]byte(g.Digest), []byte(hex.EncodeToString(digest[:]))) == 1 {
			return g.Grant, nil
		}
	}
	return Grant{}, ErrRequired
}

func (s *Store) Snapshot() ([]Request, []Grant) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.prune()
	requests, grants := []Request{}, []Grant{}
	for _, p := range s.requests {
		if p.state == "pending" {
			requests = append(requests, p.Request)
		}
	}
	for _, g := range s.grants {
		grants = append(grants, g.Grant)
	}
	return requests, grants
}

func (s *Store) hasGrant(id string) bool {
	for _, g := range s.grants {
		if g.ID == id {
			return true
		}
	}
	return false
}

func (s *Store) Revoke(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.revoke(id)
}

func (s *Store) revoke(id string) error {
	grants := make([]verifier, 0, len(s.grants))
	for _, g := range s.grants {
		if g.ID != id {
			grants = append(grants, g)
		}
	}
	if len(grants) == len(s.grants) {
		return nil
	}
	if err := s.persist(grants); err != nil {
		return err
	}
	s.grants = grants
	if s.OnRevoke != nil {
		s.OnRevoke(id)
	}
	for _, p := range s.requests {
		if p.grantID == id {
			p.token, p.state = "", "denied"
		}
	}
	return nil
}

func (s *Store) persist(grants []verifier) error {
	data, err := json.Marshal(grants)
	if err != nil {
		return ErrStorage
	}
	f, err := os.CreateTemp(s.directory, ".pairings-")
	if err != nil {
		return ErrStorage
	}
	name := f.Name()
	defer os.Remove(name)
	if err = f.Chmod(0600); err == nil {
		_, err = f.Write(data)
	}
	if err == nil {
		err = f.Sync()
	}
	closeErr := f.Close()
	if err != nil || closeErr != nil {
		return ErrStorage
	}
	if err := replace(name, filepath.Join(s.directory, "pairings.json")); err != nil {
		return ErrStorage
	}
	return nil
}
