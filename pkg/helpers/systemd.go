//go:build !windows

package helpers

import (
	"fmt"
	"net"
	"os"
)

// SDNotify sends a sd_notify message to the systemd service manager via the
// NOTIFY_SOCKET unix datagram socket. It is a silent no-op when NOTIFY_SOCKET
// is not set, so it is safe to call unconditionally on non-systemd systems.
//
// Common state strings:
//
//	"READY=1"                         — service is up and ready to serve requests
//	"WATCHDOG=1"                      — watchdog keepalive ping
//	"STOPPING=1"                      — graceful shutdown started
//	"STATUS=Serving on :8080"         — human-readable status line (visible in systemctl status)
//	"RELOADING=1\nMONOTONIC_USEC=..."  — config reload (rarely needed)
func SDNotify(state string) error {
	socketAddr := os.Getenv("NOTIFY_SOCKET")
	if socketAddr == "" {
		return nil
	}
	// Abstract namespace sockets use "@" prefix in systemd but require a NUL byte in Go.
	if socketAddr[0] == '@' {
		socketAddr = "\x00" + socketAddr[1:]
	}
	conn, err := net.Dial("unixgram", socketAddr)
	if err != nil {
		return fmt.Errorf("sd_notify: %w", err)
	}
	defer func() { _ = conn.Close() }()
	_, err = fmt.Fprint(conn, state)
	return err
}
