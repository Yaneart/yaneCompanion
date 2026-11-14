package sessions

import (
	"testing"

	"yanecompanion/internal/locator"
)

func TestLC011QuiesceExcludesAdmissionAndPreservesActiveViewing(t *testing.T) {
	m := New(nil)
	m.active = &session{snapshot: Snapshot{State: "ready"}}
	if err := m.Quiesce(); err == nil || m.closing {
		t.Fatal("active playback was interrupted")
	}
	if m.Activity() != "ready" {
		t.Fatal("changed active session")
	}
	m.active = nil
	if err := m.Quiesce(); err != nil {
		t.Fatal(err)
	}
	if _, err := m.Create("", "", "", nil, locator.Locator{}); err == nil {
		t.Fatal("admission after removal requested")
	}
}
