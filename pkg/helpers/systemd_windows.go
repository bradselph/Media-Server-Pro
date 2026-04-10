//go:build windows

package helpers

// SDNotify is a no-op on Windows (systemd/notify sockets are Unix-only).
func SDNotify(_ string) error {
	return nil
}
