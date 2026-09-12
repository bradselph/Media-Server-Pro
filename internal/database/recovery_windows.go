package database

import "os/exec"

// recoveryDetachAttrs is a no-op on Windows: a child process is not killed when
// its parent exits, so no explicit detaching is needed.
func recoveryDetachAttrs(_ *exec.Cmd) {}
