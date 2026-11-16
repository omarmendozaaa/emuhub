package remote

import (
	"os/exec"
	"syscall"
)

// configure makes sure the child does not outlive EmuHub: a Sunshine left
// behind would keep sharing the screen.
func configure(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Pdeathsig: syscall.SIGTERM}
}

func terminate(cmd *exec.Cmd) {
	cmd.Process.Signal(syscall.SIGTERM)
}
