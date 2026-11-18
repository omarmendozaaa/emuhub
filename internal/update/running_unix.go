//go:build !windows

package update

import "syscall"

func running(pid int) bool {
	err := syscall.Kill(pid, 0)
	return err == nil || err == syscall.EPERM
}
