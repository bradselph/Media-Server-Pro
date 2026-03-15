//go:build windows

package crawler

import "os/exec"

func setChromeProcessAttrs(cmd *exec.Cmd) {
	// On Windows, Kill() on the process typically terminates the job.
	// TaskKill /T could be used for full tree kill if needed.
}

func killChromeProcessGroup(cmd *exec.Cmd) {
	if cmd == nil || cmd.Process == nil {
		return
	}
	_ = cmd.Process.Kill()
	_ = cmd.Wait()
}
