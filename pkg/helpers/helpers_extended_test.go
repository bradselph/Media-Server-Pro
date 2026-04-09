package helpers

import (
	"net"
	"testing"
)

// ---------------------------------------------------------------------------
// isPrivateIP
// ---------------------------------------------------------------------------

func TestIsPrivateIP_Loopback(t *testing.T) {
	if !isPrivateIP(net.ParseIP("127.0.0.1")) {
		t.Error("127.0.0.1 should be private")
	}
}

func TestIsPrivateIP_PrivateRanges(t *testing.T) {
	privates := []string{"10.0.0.1", "172.16.0.1", "192.168.1.1", "10.255.255.255"}
	for _, ip := range privates {
		if !isPrivateIP(net.ParseIP(ip)) {
			t.Errorf("%s should be private", ip)
		}
	}
}

func TestIsPrivateIP_Public(t *testing.T) {
	publics := []string{"8.8.8.8", "1.1.1.1", "93.184.216.34"}
	for _, ip := range publics {
		if isPrivateIP(net.ParseIP(ip)) {
			t.Errorf("%s should NOT be private", ip)
		}
	}
}

func TestIsPrivateIP_IPv6Loopback(t *testing.T) {
	if !isPrivateIP(net.ParseIP("::1")) {
		t.Error("::1 should be private")
	}
}

func TestIsPrivateIP_LinkLocal(t *testing.T) {
	if !isPrivateIP(net.ParseIP("169.254.1.1")) {
		t.Error("169.254.1.1 (link-local) should be private")
	}
}

// ---------------------------------------------------------------------------
// SanitizeMap
// ---------------------------------------------------------------------------

func TestSanitizeMap_Normal(t *testing.T) {
	m := map[string]string{
		"title":  "Hello World",
		"artist": "Test Artist",
	}
	got := SanitizeMap(m)
	if got["title"] != "Hello World" {
		t.Errorf("title = %q", got["title"])
	}
}

func TestSanitizeMap_Nil(t *testing.T) {
	got := SanitizeMap(nil)
	if got != nil {
		t.Error("SanitizeMap(nil) should return nil")
	}
}

func TestSanitizeMap_HTMLEscaped(t *testing.T) {
	m := map[string]string{
		"title": "<script>alert('xss')</script>",
	}
	got := SanitizeMap(m)
	if got["title"] == "<script>alert('xss')</script>" {
		t.Error("HTML should be sanitized")
	}
}

func TestSanitizeMap_Empty(t *testing.T) {
	m := map[string]string{}
	got := SanitizeMap(m)
	if got == nil {
		t.Error("empty map should return empty map, not nil")
	}
	if len(got) != 0 {
		t.Error("should be empty")
	}
}

// ---------------------------------------------------------------------------
// ValidateURLForSSRF — scheme validation only (no DNS)
// ---------------------------------------------------------------------------

func TestValidateURLForSSRF_BadScheme(t *testing.T) {
	err := ValidateURLForSSRF("ftp://example.com/file")
	if err == nil {
		t.Error("ftp scheme should be rejected")
	}
}

func TestValidateURLForSSRF_NoScheme(t *testing.T) {
	err := ValidateURLForSSRF("example.com/path")
	if err == nil {
		t.Error("missing scheme should be rejected")
	}
}

func TestValidateURLForSSRF_InvalidURL(t *testing.T) {
	err := ValidateURLForSSRF("://bad")
	if err == nil {
		t.Error("invalid URL should be rejected")
	}
}

func TestValidateURLForSSRF_FileScheme(t *testing.T) {
	err := ValidateURLForSSRF("file:///etc/passwd")
	if err == nil {
		t.Error("file scheme should be rejected")
	}
}
