package engine

import (
	"context"
	"errors"
	"testing"
)

func TestAdmissionRemainsClosed(t *testing.T) {
	e := new(Engine)
	if err := e.Admit(context.Background(), []byte("untrusted")); !errors.Is(err, ErrAdmissionClosed) {
		t.Fatal("website work admitted before egress boundary")
	}
}
