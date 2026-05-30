//go:build windows

package workspace

import (
	"os/exec"
	"strconv"
)

// setProcessGroup is a no-op on Windows; process teardown relies on
// killProcessGroup terminating the spawned shell and its children.
func setProcessGroup(_ *exec.Cmd) {}

// killProcessGroup terminates the spawned shell and its entire child tree.
// `cmd /C` spawns the script as a child process that Process.Kill alone would
// orphan (leaving it holding the workspace directory), so taskkill /T tears
// down the whole tree; /F forces termination.
func killProcessGroup(cmd *exec.Cmd) {
	if cmd.Process == nil {
		return
	}
	_ = exec.Command("taskkill", "/T", "/F", "/PID", strconv.Itoa(cmd.Process.Pid)).Run()
	_ = cmd.Process.Kill()
}
