package engine

import (
	"context"
	"errors"
	"testing"
	"yanecompanion/internal/locator"
)

func TestUnvalidatedAdmissionRemainsClosed(t *testing.T) {
	e := new(Engine)
	if err := e.Admit(context.Background(), locator.Input{}); !errors.Is(err, locator.ErrUnsafe) {
		t.Fatal("website work admitted before egress boundary")
	}
}
