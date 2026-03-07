//go:build !windows

package handlers

import (
	"os/exec"
	"syscall"
)

// setCmdRestartAttrs detaches the replacement process from the parent's session
// so it is not killed when the parent exits (Unix: start a new session).
func setCmdRestartAttrs(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
}
