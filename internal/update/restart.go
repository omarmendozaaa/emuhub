package update

import (
	"os"
	"os/exec"
	"strconv"
	"time"
)

// waitEnv tells a new EmuHub which process it replaces. It waits for that
// one to close before opening its window, since the single-instance lock
// would otherwise hand the start over to the closing EmuHub.
const waitEnv = "EMUHUB_UPDATED_FROM"

// Restart starts exe, the updated program, with the same arguments. The
// caller then quits; the new process waits for that.
func Restart(exe string) error {
	cmd := exec.Command(exe, os.Args[1:]...)
	cmd.Env = append(os.Environ(), waitEnv+"="+strconv.Itoa(os.Getpid()))
	cmd.Stdout, cmd.Stderr = os.Stdout, os.Stderr
	if err := cmd.Start(); err != nil {
		return err
	}
	return cmd.Process.Release()
}

// WaitForPrevious waits, for a while at most, until the EmuHub this one
// replaces after an update has closed.
func WaitForPrevious() {
	pid, err := strconv.Atoi(os.Getenv(waitEnv))
	os.Unsetenv(waitEnv)
	if err != nil || pid <= 0 {
		return
	}
	deadline := time.Now().Add(20 * time.Second)
	for time.Now().Before(deadline) && running(pid) {
		time.Sleep(100 * time.Millisecond)
	}
}
