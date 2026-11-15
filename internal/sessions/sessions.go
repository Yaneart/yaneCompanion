// Package sessions owns controller leases and bounded local torrent admission.
package sessions

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"

	"yanecompanion/internal/engine"
	"yanecompanion/internal/locator"
	"yanecompanion/internal/media"
)

type Backend interface {
	Admit(context.Context, locator.Input) error
	Files(context.Context, string) ([]engine.File, error)
	Download(context.Context, string, engine.File) (int64, error)
	Drop(context.Context, string) error
}

type Error struct {
	Status int
	Code   string
}

func (e *Error) Error() string             { return e.Code }
func denied(status int, code string) error { return &Error{status, code} }

type File struct {
	Ref  string `json:"fileRef"`
	Name string `json:"name"`
	Size int64  `json:"size"`
}
type Snapshot struct {
	ID         string        `json:"sessionId"`
	Revision   int           `json:"revision"`
	State      string        `json:"state"`
	Files      []File        `json:"files"`
	Selected   string        `json:"selectedFileRef,omitempty"`
	Downloaded int64         `json:"downloadedBytes"`
	Error      string        `json:"error,omitempty"`
	Probe      *media.Probe  `json:"probe,omitempty"`
	Target     *media.Target `json:"target,omitempty"`
	Source     *Source       `json:"source,omitempty"`
}
type Admission struct {
	OperationID string `json:"operationId"`
	SessionID   string `json:"sessionId"`
	Revision    int    `json:"revision"`
	State       string `json:"state"`
}
type Operation struct {
	Admission
	State string `json:"state"`
	Error string `json:"error,omitempty"`
}
type record struct {
	fingerprint [32]byte
	admission   Admission
	operation   Operation
	expires     time.Time
}
type controller struct {
	id, origin, grant string
	expires           time.Time
	operations        map[string]*record
	sessions          map[string]*session
}
type session struct {
	ctx                 context.Context
	playback            *media.Playback
	readGrant, sourceID string
	sourceRevision      int
	snapshot            Snapshot
	owner               *controller
	hash                string
	files               map[string]engine.File
	cancel              context.CancelFunc
	done                chan struct{}
	pending             string
	selectCancel        context.CancelFunc
	selecting           bool
	selectDone          chan struct{}
}
type Manager struct {
	mu          sync.Mutex
	backend     Backend
	controllers map[[32]byte]*controller
	active      *session
	now         func() time.Time
	fatal       chan error
	media       *media.Service
	closing     bool
}
type Controller struct {
	ID        string    `json:"controllerId"`
	Token     string    `json:"controllerToken"`
	ExpiresAt time.Time `json:"expiresAt"`
}

var uuid = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`)

func New(backend Backend) *Manager {
	return &Manager{backend: backend, controllers: map[[32]byte]*controller{}, now: time.Now, fatal: make(chan error, 1)}
}
func NewWithMedia(backend Backend, service *media.Service) *Manager {
	m := New(backend)
	m.media = service
	return m
}
func (m *Manager) MediaEnabled() bool { return m.media != nil }

// Activity reveals no torrent identity, file path, controller or read grant.
func (m *Manager) Activity() string {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.active == nil {
		return ""
	}
	return m.active.snapshot.State
}

// DiagnosticError returns a code only; no file, torrent or grant identity.
func (m *Manager) DiagnosticError() string {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.active == nil {
		return ""
	}
	return m.active.snapshot.Error
}

// Quiesce atomically excludes a raced admission before a user-requested removal.
func (m *Manager) Quiesce() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.active != nil {
		return denied(409, "UPDATE_PLAYBACK_ACTIVE")
	}
	m.closing = true
	return nil
}
func (m *Manager) Errors() <-chan error { return m.fatal }
func secret(n int) string               { return hex.EncodeToString(random(n)) }
func random(n int) []byte {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		panic(err)
	}
	return b
}

func (m *Manager) CreateController(origin, grant string) (Controller, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.prune()
	if len(m.controllers) >= 8 {
		return Controller{}, denied(429, "RESOURCE_LIMIT")
	}
	token := secret(32)
	c := &controller{id: secret(16), origin: origin, grant: grant, expires: m.now().Add(180 * time.Second), operations: map[string]*record{}, sessions: map[string]*session{}}
	m.controllers[sha256.Sum256([]byte(token))] = c
	return Controller{c.id, token, c.expires}, nil
}
func (m *Manager) auth(origin, token string) (*controller, error) {
	m.prune()
	c := m.controllers[sha256.Sum256([]byte(token))]
	if len(token) != 64 || c == nil || c.origin != origin {
		return nil, denied(401, "CONTROLLER_REQUIRED")
	}
	return c, nil
}
func (m *Manager) Heartbeat(origin, token string) (time.Time, error) {
	m.mu.Lock()
	c, err := m.auth(origin, token)
	if err != nil {
		m.mu.Unlock()
		return time.Time{}, err
	}
	c.expires = m.now().Add(180 * time.Second)
	expires := c.expires
	s := m.active
	keepAlive := s != nil && s.owner == c && (s.snapshot.State == "awaiting_file" || s.snapshot.State == "awaiting_target" || s.snapshot.State == "ready")
	m.mu.Unlock()
	if keepAlive {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		_, err := m.backend.Files(ctx, s.hash)
		cancel()
		if err != nil {
			m.mu.Lock()
			if m.active == s {
				m.stop(s)
			}
			m.mu.Unlock()
			return time.Time{}, denied(503, "ENGINE_UNAVAILABLE")
		}
	}
	return expires, nil
}

func (m *Manager) Authorize(origin, token string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	_, err := m.auth(origin, token)
	return err
}
func (m *Manager) DeleteController(origin, token string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	c, err := m.auth(origin, token)
	if err != nil {
		return err
	}
	m.remove(c)
	return nil
}
func (m *Manager) remove(c *controller) {
	for key, item := range m.controllers {
		if item == c {
			delete(m.controllers, key)
		}
	}
	if m.active != nil && m.active.owner == c {
		m.stop(m.active)
	}
}
func (m *Manager) Revoke(grant string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, c := range m.controllers {
		if c.grant == grant {
			m.remove(c)
		}
	}
}
func (m *Manager) Tick() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.prune()
	if m.active != nil {
		m.updatePlayback(m.active)
	}
}
func (m *Manager) prune() {
	for _, c := range m.controllers {
		if !m.now().Before(c.expires) {
			m.remove(c)
			continue
		}
		for _, r := range c.operations {
			if !m.now().Before(r.expires) && r.operation.State != "pending" {
				r.operation = Operation{}
				r.admission = Admission{}
			}
		}
	}
}
func (m *Manager) Close() {
	m.mu.Lock()
	m.closing = true
	if m.active != nil {
		s := m.active
		m.stop(s)
		m.mu.Unlock()
		<-s.done
	} else {
		m.mu.Unlock()
	}
}

func (m *Manager) replay(c *controller, key, route string, body []byte) (*record, error) {
	if !uuid.MatchString(key) {
		return nil, denied(400, "INVALID_INPUT")
	}
	sum := sha256.Sum256(append([]byte(route+"\n"), body...))
	if prior := c.operations[key]; prior != nil {
		if prior.fingerprint != sum {
			return nil, denied(409, "IDEMPOTENCY_CONFLICT")
		}
		if prior.admission.SessionID == "" {
			return nil, denied(410, "OPERATION_EXPIRED")
		}
		return prior, nil
	}
	if len(c.operations) >= 1024 {
		return nil, denied(429, "RESOURCE_LIMIT")
	}
	return &record{fingerprint: sum}, nil
}
func (m *Manager) Create(origin, token, key string, body []byte, l locator.Locator) (Admission, error) {
	m.mu.Lock()
	if !m.closing && m.active != nil && m.active.snapshot.State == "stopping" {
		if _, err := m.auth(origin, token); err != nil {
			m.mu.Unlock()
			return Admission{}, err
		}
		// Revocation is immediate, but the old worker still owns the device
		// until its readers and engine Drop finish. Wait outside the lock;
		// all authorization, replay and ownership checks below run again.
		done := m.active.done
		m.mu.Unlock()
		timer := time.NewTimer(2 * time.Second)
		select {
		case <-done:
		case <-timer.C:
		}
		timer.Stop()
		m.mu.Lock()
	}
	defer m.mu.Unlock()
	if m.closing {
		return Admission{}, denied(503, "ENGINE_UNAVAILABLE")
	}
	c, err := m.auth(origin, token)
	if err != nil {
		return Admission{}, err
	}
	r, err := m.replay(c, key, "POST /v1/sessions", body)
	if err != nil {
		return Admission{}, err
	}
	if r.admission.SessionID != "" {
		return r.admission, nil
	}
	input, err := locator.Validate(l)
	if err != nil {
		return Admission{}, denied(400, "UNSAFE_LOCATOR")
	}
	if m.active != nil {
		return Admission{}, denied(409, "DEVICE_BUSY")
	}
	if len(c.sessions) >= 32 {
		return Admission{}, denied(429, "RESOURCE_LIMIT")
	}
	ctx, cancel := context.WithCancel(context.Background())
	s := &session{ctx: ctx, snapshot: Snapshot{ID: secret(16), Revision: 1, State: "metadata", Files: []File{}}, owner: c, hash: input.Hash(), files: map[string]engine.File{}, cancel: cancel, done: make(chan struct{}), pending: key}
	m.active = s
	c.sessions[s.snapshot.ID] = s
	r.admission = Admission{key, s.snapshot.ID, 1, "metadata"}
	r.operation = Operation{Admission: r.admission, State: "pending"}
	r.expires = m.now().Add(10 * time.Minute)
	c.operations[key] = r
	go m.load(ctx, s, input)
	return r.admission, nil
}
func (m *Manager) load(ctx context.Context, s *session, input locator.Input) {
	defer close(s.done)
	work, cancel := context.WithTimeout(ctx, 120*time.Second)
	err := m.backend.Admit(work, input)
	files := []engine.File{}
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	for err == nil {
		files, err = m.backend.Files(work, s.hash)
		if err != nil || len(files) > 0 {
			break
		}
		select {
		case <-work.Done():
			err = work.Err()
		case <-ticker.C:
		}
	}
	failureCode := "ENGINE_UNAVAILABLE"
	if errors.Is(work.Err(), context.DeadlineExceeded) {
		failureCode = "NO_PEERS"
	}
	if errors.Is(err, locator.ErrUnsafe) {
		failureCode = "UNSAFE_LOCATOR"
	}
	cancel()
	m.mu.Lock()
	if ctx.Err() == nil {
		if err != nil {
			s.snapshot.State = "failed"
			s.snapshot.Error = failureCode
			s.owner.operations[s.pending].operation.State = "failed"
			s.owner.operations[s.pending].operation.Error = failureCode
			s.cancel()
		} else {
			for _, file := range files {
				ref := secret(16)
				s.files[ref] = file
				s.snapshot.Files = append(s.snapshot.Files, File{ref, strings.ReplaceAll(file.Path, "\\", "/"), file.Length})
			}
			s.snapshot.State = "awaiting_file"
			s.owner.operations[s.pending].operation.State = "ready"
			s.pending = ""
		}
	}
	m.mu.Unlock()
	<-ctx.Done()
	m.mu.Lock()
	downloadDone := s.selectDone
	if s.selectCancel != nil {
		s.selectCancel()
	}
	m.mu.Unlock()
	if downloadDone != nil {
		<-downloadDone
	}
	m.mu.Lock()
	playback := s.playback
	s.playback = nil
	s.readGrant = ""
	s.snapshot.Source = nil
	m.mu.Unlock()
	if playback != nil {
		playback.Close()
	}
	// A cancelled engine POST may still complete remotely. Drop after it settles,
	// retaining device ownership throughout cleanup so a replacement cannot race.
	cleanup, finish := context.WithTimeout(context.Background(), 5*time.Second)
	defer finish()
	dropErr := m.backend.Drop(cleanup, s.hash)
	m.mu.Lock()
	defer m.mu.Unlock()
	if dropErr != nil {
		select {
		case m.fatal <- engine.ErrUnavailable:
		default:
		}
		return
	}
	if s.snapshot.State == "stopping" {
		s.snapshot.State = "stopped"
	}
	if m.active == s {
		m.active = nil
	}
}
func (m *Manager) Snapshot(origin, token, id string) (Snapshot, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	c, err := m.auth(origin, token)
	if err != nil {
		return Snapshot{}, err
	}
	s := c.sessions[id]
	if id == "active" {
		s = nil
		if m.active != nil && m.active.owner == c {
			s = m.active
		}
	}
	if s == nil {
		return Snapshot{}, denied(404, "NOT_FOUND")
	}
	m.updatePlayback(s)
	result := s.snapshot
	result.Files = append([]File{}, s.snapshot.Files...)
	return result, nil
}
func (m *Manager) Operation(origin, token, key string) (Operation, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	c, err := m.auth(origin, token)
	if err != nil {
		return Operation{}, err
	}
	r := c.operations[key]
	if r == nil {
		return Operation{}, denied(404, "NOT_FOUND")
	}
	if r.admission.SessionID == "" {
		return Operation{}, denied(410, "OPERATION_EXPIRED")
	}
	return r.operation, nil
}
func (m *Manager) stop(s *session) {
	if s.snapshot.State == "stopped" || s.snapshot.State == "stopping" {
		return
	}
	if m.active != s {
		s.snapshot.State = "stopped"
		return
	}
	s.snapshot.State = "stopping"
	s.snapshot.Source = nil
	s.readGrant = ""
	s.cancel()
	if s.selectCancel != nil {
		s.selectCancel()
	}
	if s.pending != "" {
		s.owner.operations[s.pending].operation.State = "cancelled"
		s.pending = ""
	}
}
func (m *Manager) Stop(origin, token, id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	c, err := m.auth(origin, token)
	if err != nil {
		return err
	}
	s := c.sessions[id]
	if s == nil {
		return denied(404, "NOT_FOUND")
	}
	m.stop(s)
	return nil
}
func (m *Manager) Cancel(origin, token, key string, revision int) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	c, err := m.auth(origin, token)
	if err != nil {
		return err
	}
	r := c.operations[key]
	if r == nil {
		return denied(404, "NOT_FOUND")
	}
	if r.admission.SessionID == "" {
		return denied(410, "OPERATION_EXPIRED")
	}
	s := c.sessions[r.admission.SessionID]
	if revision != s.snapshot.Revision {
		return denied(409, "REVISION_CONFLICT")
	}
	if r.operation.State == "cancelled" {
		return nil
	}
	if r.operation.State != "pending" {
		return denied(409, "REVISION_CONFLICT")
	}
	s.snapshot.Revision++
	if m.media != nil && (s.snapshot.State == "probing" || s.snapshot.State == "preparing") {
		if s.selectCancel != nil {
			s.selectCancel()
		}
		if s.snapshot.State == "probing" {
			s.snapshot.State = "awaiting_file"
		} else {
			s.snapshot.State = "awaiting_target"
		}
		s.pending = ""
		s.snapshot.Source = nil
		s.readGrant = ""
		r.operation.State = "cancelled"
		return nil
	}
	m.stop(s)
	return nil
}
func (m *Manager) Select(origin, token, id, key string, body []byte, revision int, ref string) (Admission, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	c, err := m.auth(origin, token)
	if err != nil {
		return Admission{}, err
	}
	r, err := m.replay(c, key, "POST /v1/sessions/"+id+"/commands", body)
	if err != nil {
		return Admission{}, err
	}
	if r.admission.SessionID != "" {
		return r.admission, nil
	}
	s := c.sessions[id]
	if s == nil {
		return Admission{}, denied(404, "NOT_FOUND")
	}
	if s.snapshot.Revision != revision || s.pending != "" || s.selecting || s.snapshot.State != "awaiting_file" {
		return Admission{}, denied(409, "REVISION_CONFLICT")
	}
	file, ok := s.files[ref]
	if !ok || file.Length < 1 {
		return Admission{}, denied(400, "INVALID_INPUT")
	}
	if !video(file.Path) {
		return Admission{}, denied(422, "UNSUPPORTED_MEDIA")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	s.selectCancel = cancel
	s.selectDone = make(chan struct{})
	done := s.selectDone
	s.selecting = true
	s.pending = key
	s.snapshot.Revision++
	s.snapshot.Selected = ref
	s.snapshot.Downloaded = 0
	s.snapshot.Error = ""
	r.admission = Admission{key, id, s.snapshot.Revision, s.snapshot.State}
	r.operation = Operation{Admission: r.admission, State: "pending"}
	r.expires = m.now().Add(10 * time.Minute)
	c.operations[key] = r
	go func() {
		defer close(done)
		defer cancel()
		n, err := m.backend.Download(ctx, s.hash, file)
		m.mu.Lock()
		defer m.mu.Unlock()
		s.selecting = false
		if s.pending != key || s.snapshot.State != "awaiting_file" {
			return
		}
		s.pending = ""
		if err != nil {
			r.operation.State = "failed"
			code := "ENGINE_UNAVAILABLE"
			if errors.Is(ctx.Err(), context.DeadlineExceeded) {
				code = "NO_PEERS"
			}
			r.operation.Error = code
			s.snapshot.Error = code
		} else {
			r.operation.State = "ready"
			s.snapshot.Downloaded = n
		}
	}()
	return r.admission, nil
}
func video(name string) bool {
	switch strings.ToLower(filepath.Ext(name)) {
	case ".mkv", ".mp4", ".m4v", ".avi", ".mov", ".ts", ".webm":
		return true
	}
	return false
}

// Code returns only stable public errors; engine URLs and input never leave here.
func Code(err error) (int, string) {
	var e *Error
	if errors.As(err, &e) {
		return e.Status, e.Code
	}
	return 503, "ENGINE_UNAVAILABLE"
}
