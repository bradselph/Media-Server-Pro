package security

import (
	"net"
	"testing"
)

// TestIsTrustedProxyIP directly exercises the proxy-trust predicate extracted
// from getClientIP, including the two paths the getClientIP tests only cover
// transitively: a nil IP (never trusted) and an IP trusted solely via the
// admin-configured extraTrusted list (not in the hardcoded private ranges).
func TestIsTrustedProxyIP(t *testing.T) {
	_, extra, err := net.ParseCIDR("203.0.113.0/24") // TEST-NET-3, public
	if err != nil {
		t.Fatalf("ParseCIDR: %v", err)
	}
	extraTrusted := []*net.IPNet{extra}

	tests := []struct {
		name  string
		ip    string // "" means a nil net.IP
		extra []*net.IPNet
		want  bool
	}{
		{"nil ip is never trusted", "", extraTrusted, false},
		{"IPv4 loopback is private", "127.0.0.1", nil, true},
		{"10/8 is private", "10.1.2.3", nil, true},
		{"172.16/12 is private", "172.16.5.5", nil, true},
		{"192.168/16 is private", "192.168.1.1", nil, true},
		{"IPv6 loopback is private", "::1", nil, true},
		{"IPv6 ULA fc00::/7 is private", "fc00::1", nil, true},
		{"public IP not trusted without extra", "203.0.113.10", nil, false},
		{"public IP trusted via extraTrusted only", "203.0.113.10", extraTrusted, true},
		{"public IP outside extraTrusted not trusted", "198.51.100.1", extraTrusted, false},
		{"private IP trusted even with unrelated extra", "10.0.0.9", extraTrusted, true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var ip net.IP
			if tc.ip != "" {
				if ip = net.ParseIP(tc.ip); ip == nil {
					t.Fatalf("test IP %q failed to parse", tc.ip)
				}
			}
			if got := isTrustedProxyIP(ip, tc.extra); got != tc.want {
				t.Errorf("isTrustedProxyIP(%q, hasExtra=%v) = %v, want %v",
					tc.ip, tc.extra != nil, got, tc.want)
			}
		})
	}
}
