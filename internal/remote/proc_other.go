//go:build !linux && !windows

package remote

import (
	"os/exec"
	"syscall"
)

func configure(cmd *exec.Cmd) {}

func terminate(cmd *exec.Cmd) {
	cmd.Process.Signal(syscall.SIGTERM)
}
