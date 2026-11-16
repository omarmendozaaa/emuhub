package remote

import (
	"os/exec"
	"syscall"
)

const createNoWindow = 0x08000000

// configure keeps Sunshine, a console program, from opening a terminal.
func configure(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: createNoWindow}
}

func terminate(cmd *exec.Cmd) {
	cmd.Process.Kill()
}
