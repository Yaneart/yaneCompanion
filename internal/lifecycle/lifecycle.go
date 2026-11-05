package lifecycle

import (
	"errors"
	"os/exec"
	"runtime"
	"sync"
)

var ErrRunning = errors.New("ALREADY_RUNNING")

type Process struct {
	cmd  *exec.Cmd
	done chan struct{}
	err  error
	once sync.Once
}

// Start runs a structured command without a shell. Its caller verifies the
// executable and bounds output; worker stderr must not become diagnostics.
func Start(cmd *exec.Cmd) (*Process, error) {
	prepare(cmd)
	p := &Process{cmd: cmd, done: make(chan struct{})}
	started := make(chan error, 1)
	go func() {
		// Linux Pdeathsig follows the creating OS thread. Keep that thread alive
		// for the whole worker lifetime, including while the caller is elsewhere.
		runtime.LockOSThread()
		defer runtime.UnlockOSThread()
		if err := cmd.Start(); err != nil {
			started <- err
			return
		}
		started <- nil
		p.err = cmd.Wait()
		close(p.done)
	}()
	if err := <-started; err != nil {
		return nil, err
	}
	return p, nil
}

func (p *Process) Done() <-chan struct{} { return p.done }
func (p *Process) Wait() error           { <-p.done; return p.err }
func (p *Process) PID() int              { return p.cmd.Process.Pid }
func (p *Process) Stop() {
	p.once.Do(func() {
		select {
		case <-p.done:
			return
		default:
		}
		kill(p.cmd.Process)
		<-p.done
	})
}
