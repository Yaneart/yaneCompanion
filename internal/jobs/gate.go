package jobs

import (
	"context"
	"errors"
)

var ErrLimit = errors.New("RESOURCE_LIMIT")

// One active job and one waiting job per lane. Callers retain cancellation;
// retries do not accumulate unbounded goroutines or start detached workers.
type Gate struct {
	active   chan struct{}
	admitted chan struct{}
}

func New() *Gate { return &Gate{make(chan struct{}, 1), make(chan struct{}, 2)} }
func (g *Gate) Run(ctx context.Context, job func(context.Context) error) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	select {
	case g.admitted <- struct{}{}:
	default:
		return ErrLimit
	}
	defer func() { <-g.admitted }()
	select {
	case g.active <- struct{}{}:
	case <-ctx.Done():
		return ctx.Err()
	}
	defer func() { <-g.active }()
	if err := ctx.Err(); err != nil {
		return err
	}
	return job(ctx)
}
