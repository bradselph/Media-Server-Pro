//go:build windows

package crawler

import (
	"os/exec"
	"strconv"
)

func setChromeProcessAttrs(_ *exec.Cmd) {
	// No special process attributes needed on Windows; tree kill is handled
	// in killChromeProcessGroup via taskkill /T.
}

func killChromeProcessGroup(cmd *exec.Cmd) {
	if cmd == nil || cmd.Process == nil {
		return
	}
	// Use taskkill /T /F to kill the entire process tree (parent + children).
	// This prevents orphaned Chrome child processes on Windows.
	pid := strconv.Itoa(cmd.Process.Pid)
	if err := exec.Command("taskkill", "/T", "/F", "/PID", pid).Run(); err != nil { //nolint:gosec // G204: taskkill is a Windows system binary, pid is from cmd.Process.Pid
		// Fallback to killing just the parent process if taskkill fails
		_ = cmd.Process.Kill()
	}
	_ = cmd.Wait()
}
