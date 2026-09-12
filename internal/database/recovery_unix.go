//go:build !windows

package database

import (
	"os/exec"
	"syscall"
)

// recoveryDetachAttrs puts the recovery command in its own session so it is not
// killed when this process exits or when its controlling terminal goes away.
//
// This is necessary but not sufficient under systemd: a new session does not
// leave the unit's cgroup, so `systemctl stop` on this unit still reaches the
// command. systemdRunPrefix handles that case by having PID 1 start the command
// instead; this covers every other way the server gets run.
func recoveryDetachAttrs(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
}
