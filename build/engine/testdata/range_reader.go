package torrstor

import (
	"context"
	"io"
)

// Set once before ServeContent starts reading. A disconnected media job must
// stop waiting on peers and release its cache reader and piece priorities.
func (r *Reader) SetRequestContext(ctx context.Context) {
	r.readContext = func(p []byte) (int, error) { return r.Reader.ReadContext(ctx, p) }
}

func (r *Reader) readRequest(p []byte) (int, error) {
	if r.bounded {
		if r.offset < r.rangeStart || r.offset >= r.rangeEnd {
			return 0, io.EOF
		}
		p = p[:min(int64(len(p)), r.rangeEnd-r.offset)]
	}
	if r.readContext != nil {
		return r.readContext(p)
	}
	return r.Reader.Read(p)
}
