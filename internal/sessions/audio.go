package sessions

import (
	"context"
	"time"
)

// Called under Manager.mu after auth, replay, ownership and revision checks.
// Audio preparation leaves the source revision/read grant/video intact.
func (m *Manager) selectAudio(s *session, c *controller, r *record, key string, command Command) (Admission, error) {
	target := command.Target
	if target == nil || command.FileRef != "" || command.ClientMedia != nil || target.FileRef != s.snapshot.Selected || target.AudioMode != "separate" || target.TextRef != nil || target.Output != "original" {
		return Admission{}, denied(400, "INVALID_INPUT")
	}
	if s.snapshot.State != "ready" || s.playback == nil || !s.playback.SeparateAudio() {
		return Admission{}, denied(409, "REVISION_CONFLICT")
	}
	ctx, cancel := context.WithCancel(s.ctx)
	s.selectCancel = cancel
	s.selectDone = make(chan struct{})
	done := s.selectDone
	s.selecting, s.audioSelecting, s.pending = true, true, key
	s.snapshot.Revision++
	r.admission = Admission{key, s.snapshot.ID, s.snapshot.Revision, "ready"}
	r.operation = Operation{Admission: r.admission, State: "pending"}
	r.expires = m.now().Add(10 * time.Minute)
	c.operations[key] = r
	playback := s.playback
	wanted := *target
	go func() {
		defer close(done)
		// The selected producer lives with the playback, not the command waiter.
		value, err := playback.SelectAudio(ctx, wanted.AudioRef, wanted.Position)
		m.mu.Lock()
		defer m.mu.Unlock()
		s.selecting, s.audioSelecting = false, false
		if ctx.Err() != nil || s.pending != key || s.playback != playback || s.snapshot.State != "ready" {
			return
		}
		s.pending = ""
		if err != nil {
			r.operation.State, r.operation.Error = "failed", "MEDIA_FAILED"
			cancel()
			return
		}
		r.operation.State = "ready"
		if value.Position != nil {
			wanted.Position = *value.Position
		}
		s.snapshot.Target = &wanted
		m.updatePlayback(s)
	}()
	return r.admission, nil
}
