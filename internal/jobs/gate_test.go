package jobs

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"
)

func TestQueueBoundAndCancellation(t *testing.T) {
	g := New()
	started, release, done := make(chan struct{}), make(chan struct{}), make(chan error, 2)
	go func() {
		done <- g.Run(context.Background(), func(context.Context) error { close(started); <-release; return nil })
	}()
	<-started
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var unexpected atomic.Bool
	go func() { done <- g.Run(ctx, func(context.Context) error { unexpected.Store(true); return nil }) }()
	deadline := time.Now().Add(time.Second)
	for len(g.admitted) < 2 && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if err := g.Run(context.Background(), func(context.Context) error { return nil }); !errors.Is(err, ErrLimit) {
		t.Fatalf("unbounded admission: %v", err)
	}
	cancel()
	if err := <-done; !errors.Is(err, context.Canceled) {
		t.Fatalf("queued cancellation: %v", err)
	}
	close(release)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if unexpected.Load() {
		t.Fatal("cancelled queued work started")
	}
	if err := g.Run(context.Background(), func(context.Context) error { return nil }); err != nil {
		t.Fatal("capacity not released:", err)
	}
}
