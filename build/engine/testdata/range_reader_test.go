package torrstor

import (
	"context"
	"errors"
	"github.com/anacrolix/torrent"
	"io"
	"testing"
	"time"
)

type companionReadAhead struct {
	torrent.Reader
	ahead int64
	size  int
	block bool
}

func (r *companionReadAhead) SetReadahead(n int64) { r.ahead = n }
func (r *companionReadAhead) ReadContext(ctx context.Context, p []byte) (int, error) {
	r.size = len(p)
	if r.block {
		<-ctx.Done()
		return 0, ctx.Err()
	}
	return len(p), nil
}

func TestCompanionFiniteRangePriorities(t *testing.T) {
	underlying := &companionReadAhead{}
	r := &Reader{Reader: underlying, isUse: true, bounded: true, rangeStart: 123, rangeEnd: 456}
	// The torrent's periodic 16MiB adjustment must not expand a finite range.
	r.SetReadahead(16 << 20)
	if underlying.ahead != 0 || r.Readahead() != 0 {
		t.Fatal("finite reader prefetched outside request")
	}
	start, end := r.getOffsetRange()
	if start != 123 || end != 455 {
		t.Fatalf("priority/cache range expanded: %d..%d", start, end)
	}
	r.bounded = false
	r.SetReadahead(16 << 20)
	if underlying.ahead != 16<<20 {
		t.Fatal("ordinary reader lost read-ahead")
	}
}

func TestCompanionRangeReadAndCancel(t *testing.T) {
	underlying := &companionReadAhead{}
	r := &Reader{Reader: underlying, bounded: true, rangeStart: 123, rangeEnd: 456, offset: 450}
	r.SetRequestContext(context.Background())
	if n, err := r.readRequest(make([]byte, 1<<20)); err != nil || n != 6 || underlying.size != 6 {
		t.Fatal("buffer read beyond range", n, err)
	}
	r.offset = 456
	if _, err := r.readRequest(make([]byte, 1)); !errors.Is(err, io.EOF) {
		t.Fatal("range EOF", err)
	}
	r.offset = 0
	if _, err := r.readRequest(make([]byte, 1)); !errors.Is(err, io.EOF) {
		t.Fatal("range sniff escaped", err)
	}
	r.offset = 123
	underlying.block = true
	ctx, cancel := context.WithCancel(context.Background())
	r.SetRequestContext(ctx)
	done := make(chan error, 1)
	go func() { _, err := r.readRequest(make([]byte, 1)); done <- err }()
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("cancelled range still waits for peers")
	}
}
