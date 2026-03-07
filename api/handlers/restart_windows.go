//go:build windows

package handlers

import "os/exec"

// setCmdRestartAttrs is a no-op on Windows; the child runs in its own process group
// by default when started via exec.Cmd.
func setCmdRestartAttrs(cmd *exec.Cmd) {}
