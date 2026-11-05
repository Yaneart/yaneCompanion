package lifecycle

import (
	"os"
	"os/exec"
	"syscall"
)

func ContainHost() error { return nil }
func prepare(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true, Pdeathsig: syscall.SIGKILL}
}
func kill(process *os.Process) { _ = syscall.Kill(-process.Pid, syscall.SIGKILL) }
