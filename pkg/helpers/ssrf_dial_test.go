package helpers

import (
	"context"
	"testing"
)

// TestSafeDialContext_RejectsPrivateAddresses guards the SSRF dial hook used by
// SafeHTTPTransport and the follower's websocket dialer. It must refuse to
// connect to private/loopback/link-local/reserved addresses so a DNS rebind
// (public at validate time, internal at connect time) can't reach an internal
// service. Literal private IPs are resolved without network I/O and rejected
// before any dial, so this test needs no network.
func TestSafeDialContext_RejectsPrivateAddresses(t *testing.T) {
	blocked := []string{
		"127.0.0.1:80",       // loopback
		"169.254.169.254:80", // link-local (cloud metadata)
		"10.0.0.5:443",       // RFC-1918
		"192.168.1.10:8080",  // RFC-1918
		"[::1]:80",           // IPv6 loopback
	}
	for _, addr := range blocked {
		if _, err := SafeDialContext(context.Background(), "tcp", addr); err == nil {
			t.Errorf("SafeDialContext(%q) = nil error; a private/reserved address must be rejected", addr)
		}
	}
}
