package analytics

import (
	"testing"
)

// ---------------------------------------------------------------------------
// maskIP
// ---------------------------------------------------------------------------

func TestMaskIP_IPv4(t *testing.T) {
	got := maskIP("192.168.1.100")
	if got != "192.168.1.0" {
		t.Errorf("maskIP(192.168.1.100) = %q, want 192.168.1.0", got)
	}
}

func TestMaskIP_IPv4_Public(t *testing.T) {
	got := maskIP("8.8.8.8")
	if got != "8.8.8.0" {
		t.Errorf("maskIP(8.8.8.8) = %q, want 8.8.8.0", got)
	}
}

func TestMaskIP_IPv6(t *testing.T) {
	got := maskIP("2001:0db8:85a3:0000:0000:8a2e:0370:7334")
	// Should preserve the /64 prefix and zero the last 8 bytes
	if got == "" {
		t.Error("maskIP should return non-empty for IPv6")
	}
	// The masked IP should not equal the original
	if got == "2001:0db8:85a3:0000:0000:8a2e:0370:7334" {
		t.Error("IPv6 should be masked")
	}
}

func TestMaskIP_Invalid(t *testing.T) {
	got := maskIP("not-an-ip")
	if got != "masked" {
		t.Errorf("maskIP(invalid) = %q, want 'masked'", got)
	}
}

func TestMaskIP_Empty(t *testing.T) {
	got := maskIP("")
	if got != "masked" {
		t.Errorf("maskIP(empty) = %q, want 'masked'", got)
	}
}

func TestMaskIP_Loopback(t *testing.T) {
	got := maskIP("127.0.0.1")
	if got != "127.0.0.0" {
		t.Errorf("maskIP(127.0.0.1) = %q, want 127.0.0.0", got)
	}
}

// ---------------------------------------------------------------------------
// generateEventID
// ---------------------------------------------------------------------------

func TestGenerateEventID_Length(t *testing.T) {
	id := generateEventID()
	if len(id) != 32 {
		t.Errorf("generateEventID length = %d, want 32", len(id))
	}
}

func TestGenerateEventID_Unique(t *testing.T) {
	ids := make(map[string]bool)
	for i := 0; i < 100; i++ {
		id := generateEventID()
		if ids[id] {
			t.Fatalf("duplicate event ID: %s", id)
		}
		ids[id] = true
	}
}
