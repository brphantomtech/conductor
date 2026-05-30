//go:build !windows

package workspace

import (
	"os/exec"
	"syscall"
)

// setProcessGroup places the hook process in its own process group so a
// timeout can reap the whole tree (the script plus any children it spawned).
func setProcessGroup(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
}

// killProcessGroup signals the process group created by setProcessGroup. The
// negative PID targets every member of the group.
func killProcessGroup(cmd *exec.Cmd) {
	if cmd.Process == nil {
		return
	}
	_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
}
