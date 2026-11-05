package media

import (
	"bytes"
	"context"
	"errors"
	"os/exec"
	"time"

	"yanecompanion/internal/jobs"
	"yanecompanion/internal/lifecycle"
)

type Runner struct {
	metadata, preparation *jobs.Gate
	ffmpeg, ffprobe       string
}

func New(ffmpeg, ffprobe string) *Runner {
	return &Runner{metadata: jobs.New(), preparation: jobs.New(), ffmpeg: ffmpeg, ffprobe: ffprobe}
}

func (r *Runner) Probe(ctx context.Context, args []string) ([]byte, error) {
	return r.run(ctx, r.ffprobe, args, r.metadata)
}
func (r *Runner) Prepare(ctx context.Context, args []string) ([]byte, error) {
	return r.run(ctx, r.ffmpeg, args, r.preparation)
}

type boundedOutput struct {
	buffer   bytes.Buffer
	overflow chan struct{}
	limit    int
}

func (w *boundedOutput) Write(data []byte) (int, error) {
	if len(data) > w.limit-w.buffer.Len() {
		select {
		case w.overflow <- struct{}{}:
		default:
		}
		return 0, errors.New("OUTPUT_LIMIT")
	}
	return w.buffer.Write(data)
}

// The composition root supplies verified bundled tools. Structured worker args
// are internal application data and are never accepted over HTTP.
func (r *Runner) run(ctx context.Context, executable string, args []string, gate *jobs.Gate) ([]byte, error) {
	var output []byte
	err := gate.Run(ctx, func(ctx context.Context) error {
		ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
		defer cancel()
		stdout := &boundedOutput{overflow: make(chan struct{}, 1), limit: 1 << 20}
		stderr := &boundedOutput{overflow: stdout.overflow, limit: 64 << 10}
		cmd := exec.Command(executable, args...)
		cmd.Stdout = stdout
		cmd.Stderr = stderr
		worker, err := lifecycle.Start(cmd)
		if err != nil {
			return errors.New("MEDIA_UNAVAILABLE")
		}
		defer worker.Stop()
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-stdout.overflow:
			return errors.New("OUTPUT_LIMIT")
		case <-worker.Done():
			if err := ctx.Err(); err != nil {
				return err
			}
			select {
			case <-stdout.overflow:
				return errors.New("OUTPUT_LIMIT")
			default:
			}
			if worker.Wait() != nil {
				return errors.New("MEDIA_FAILED")
			}
			output = append([]byte(nil), stdout.buffer.Bytes()...)
			return nil
		}
	})
	return output, err
}
