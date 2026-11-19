package sessions

import (
	"context"
	"crypto/subtle"
	"errors"
	"math"
	"sort"
	"time"

	"yanecompanion/internal/config"
	"yanecompanion/internal/engine"
	"yanecompanion/internal/media"
)

type Source struct {
	ID             string                `json:"id"`
	Revision       int                   `json:"revision"`
	Type           string                `json:"type"`
	URL            string                `json:"url"`
	Audio          []media.Audio         `json:"audioTracks"`
	Text           []TextResource        `json:"textTracks"`
	TextWindow     int                   `json:"textWindowSeconds"`
	AudioMode      string                `json:"audioMode,omitempty"`
	AudioRendition *media.AudioRendition `json:"audioRendition,omitempty"`
	media.PlaybackStatus
}
type TextResource struct {
	media.Text
	URL   string   `json:"url"`
	Fonts []string `json:"fonts"`
}
type Command struct {
	Action      string              `json:"action"`
	Revision    int                 `json:"expectedRevision"`
	FileRef     string              `json:"fileRef,omitempty"`
	Target      *media.Target       `json:"target,omitempty"`
	ClientMedia []media.ClientMedia `json:"clientMedia,omitempty"`
}

func (m *Manager) Command(origin, token, id, key string, body []byte, command Command) (Admission, error) {
	if m.media == nil {
		return Admission{}, denied(422, "UNSUPPORTED_CAPABILITY")
	}
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
	if m.storageFull {
		return Admission{}, denied(507, "CACHE_FULL")
	}
	s := c.sessions[id]
	if s == nil {
		return Admission{}, denied(404, "NOT_FOUND")
	}
	if m.active != s || s.snapshot.Revision != command.Revision || s.pending != "" || s.selecting {
		return Admission{}, denied(409, "REVISION_CONFLICT")
	}
	if s.snapshot.State != "awaiting_file" && s.snapshot.State != "awaiting_target" && s.snapshot.State != "ready" {
		return Admission{}, denied(409, "REVISION_CONFLICT")
	}
	ref := command.FileRef
	state := "probing"
	switch command.Action {
	case "audio":
		return m.selectAudio(s, c, r, key, command)
	case "probe":
		if command.Target != nil || command.ClientMedia != nil {
			return Admission{}, denied(400, "INVALID_INPUT")
		}
	case "configure":
		if command.Target == nil || command.FileRef != "" {
			return Admission{}, denied(400, "INVALID_INPUT")
		}
		ref = command.Target.FileRef
		state = "preparing"
		if s.snapshot.Probe == nil || ref != s.snapshot.Selected {
			return Admission{}, denied(409, "PROBE_REQUIRED")
		}
		if _, _, err := s.snapshot.Probe.Validate(*command.Target, command.ClientMedia); err != nil {
			return Admission{}, denied(422, "UNSUPPORTED_MEDIA")
		}
	default:
		return Admission{}, denied(422, "UNSUPPORTED_CAPABILITY")
	}
	file, ok := s.files[ref]
	if !ok || file.Length < 1 {
		return Admission{}, denied(400, "INVALID_INPUT")
	}
	if !video(file.Path) {
		return Admission{}, denied(422, "UNSUPPORTED_MEDIA")
	}
	files := make([]engine.File, 0, len(s.files))
	for _, f := range s.files {
		files = append(files, f)
	}
	sort.Slice(files, func(i, j int) bool { return files[i].Index < files[j].Index })
	previous := s.playback
	if s.selectCancel != nil {
		s.selectCancel()
	}
	s.playback = nil
	s.readGrant = ""
	s.snapshot.Source = nil
	s.snapshot.Revision++
	s.snapshot.State = state
	previousRef := s.snapshot.Selected
	s.snapshot.Selected = ref
	s.snapshot.Error = ""
	s.snapshot.Downloaded = 0
	probe := s.snapshot.Probe
	if command.Action == "probe" {
		s.snapshot.Probe = nil
		s.snapshot.Target = nil
		if previousRef != ref || s.sourceID == "" {
			s.sourceID = secret(16)
			// Progress is scoped by session + sourceRevision. Keep that proof
			// monotonic so a delayed previous-file report cannot match this file.
		}
	} else {
		target := *command.Target
		s.snapshot.Target = &target
	}
	ctx, cancel := context.WithCancel(s.ctx)
	s.selectCancel = cancel
	s.selectDone = make(chan struct{})
	done := s.selectDone
	s.selecting = true
	s.pending = key
	r.admission = Admission{key, id, s.snapshot.Revision, state}
	r.operation = Operation{Admission: r.admission, State: "pending"}
	r.expires = m.now().Add(10 * time.Minute)
	c.operations[key] = r
	go func() {
		defer close(done)
		if previous != nil {
			previous.Close()
		}
		var err error
		var playback *media.Playback
		if command.Action == "probe" {
			probe, err = m.media.Probe(ctx, s.hash, file)
			if err == nil {
				probe.AddSidecars(file, files)
			}
		} else {
			playback, err = m.media.Prepare(ctx, s.hash, file, probe, *command.Target, command.ClientMedia)
		}
		m.mu.Lock()
		s.selecting = false
		if ctx.Err() != nil || s.pending != key || s.snapshot.State != state {
			m.mu.Unlock()
			if playback != nil {
				playback.Close()
			}
			cancel()
			return
		}
		s.pending = ""
		if err != nil {
			code := mediaFailure(err)
			m.lastError = code
			r.operation.State = "failed"
			r.operation.Error = code
			s.snapshot.Error = code
			if command.Action == "probe" {
				s.snapshot.State = "awaiting_file"
			} else {
				s.snapshot.State = "awaiting_target"
			}
			m.mu.Unlock()
			cancel()
			return
		}
		r.operation.State = "ready"
		m.lastError = ""
		if command.Action == "probe" {
			s.snapshot.Probe = probe
			s.snapshot.State = "awaiting_target"
			cancel()
		} else {
			s.playback = playback
			timeline := playback.Status().Timeline
			seekEnd := timeline.Duration
			if timeline.VideoEnd > 0 {
				seekEnd = math.Min(seekEnd, timeline.VideoEnd)
			}
			s.snapshot.Target.Position = math.Min(s.snapshot.Target.Position, math.Max(0, seekEnd-.01))
			s.readGrant = secret(32)
			s.sourceRevision++
			s.snapshot.State = "ready"
			base := "http://" + config.Address + "/v1/media/" + s.readGrant + "/"
			text := make([]TextResource, 0, len(probe.Text))
			for _, t := range probe.Text {
				resource := TextResource{Text: t, Fonts: []string{}}
				// Every player descriptor needs a URL, including disabled tracks.
				// Unavailable resources still reject reads without starting a worker.
				resource.URL = base + "text-" + t.Ref + "." + t.Format
				if t.Available && t.Format == "ass" {
					for _, f := range probe.Fonts {
						resource.Fonts = append(resource.Fonts, base+"font-"+f.Ref+".bin")
					}
				}
				text = append(text, resource)
			}
			s.snapshot.Source = &Source{ID: s.sourceID, Revision: s.sourceRevision, Type: "hls", URL: base + "master.m3u8", Audio: probe.Audio, Text: text, TextWindow: 30, AudioMode: command.Target.AudioMode, AudioRendition: playback.AudioRendition(), PlaybackStatus: playback.Status()}
		}
		m.mu.Unlock()
	}()
	return r.admission, nil
}

func mediaFailure(err error) string {
	if errors.Is(err, media.ErrUnsupported) {
		return "UNSUPPORTED_MEDIA"
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return "PREPARATION_TIMEOUT"
	}
	if err != nil {
		switch err.Error() {
		case "NO_PEERS", "RESOURCE_LIMIT", "CACHE_FULL", "UNSUPPORTED_MEDIA":
			return err.Error()
		case "OUTPUT_LIMIT":
			return "RESOURCE_LIMIT"
		}
	}
	return "MEDIA_FAILED"
}

func (m *Manager) updatePlayback(s *session) {
	if s.playback == nil || s.snapshot.State != "ready" || s.snapshot.Source == nil {
		return
	}
	status := s.playback.Status()
	source := *s.snapshot.Source
	source.PlaybackStatus = status
	source.AudioRendition = s.playback.AudioRendition()
	s.snapshot.Source = &source
	if status.Error != "" {
		m.fail(s, status.Error)
	}
}

func (m *Manager) ReadMedia(origin, grant string) (*media.Playback, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.prune()
	s := m.active
	if s == nil || s.owner.origin != origin || len(grant) != 64 || subtle.ConstantTimeCompare([]byte(grant), []byte(s.readGrant)) != 1 || s.snapshot.State != "ready" || s.playback == nil {
		return nil, denied(404, "NOT_FOUND")
	}
	return s.playback, nil
}

func (m *Manager) Progress(origin, token, id string, revision int, position float64, textDelay ...float64) error {
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
	if s.snapshot.State != "ready" || s.playback == nil || s.sourceRevision != revision {
		return denied(409, "REVISION_CONFLICT")
	}
	if err := s.playback.Confirm(position, textDelay...); err != nil {
		return denied(400, "INVALID_POSITION")
	}
	return nil
}
