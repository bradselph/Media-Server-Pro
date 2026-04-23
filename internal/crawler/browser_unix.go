//go:build !windows

package crawler

import (
	"os/exec"
	"syscall"
)

// setChromeProcessAttrs puts Chrome in its own process group so we can kill
// the entire tree (Chrome + renderer, GPU, etc.) on cleanup.
func setChromeProcessAttrs(cmd *exec.Cmd) {
	if cmd == nil {
		return
	}
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
}

// killChromeProcessGroup kills the Chrome process and all its children.
func killChromeProcessGroup(cmd *exec.Cmd) {
	if cmd == nil || cmd.Process == nil {
		return
	}
	// Negative PID = process group
	_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
	_ = cmd.Wait()
}
