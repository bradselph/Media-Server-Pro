package handlers

import "testing"

// TestRedactSensitiveConfigKeys covers the audit-log redaction helper: every
// sensitive keyword (case-insensitive, substring) must be redacted, non-sensitive
// values pass through untouched, and nested maps are redacted recursively.
func TestRedactSensitiveConfigKeys(t *testing.T) {
	t.Run("nil input returns nil", func(t *testing.T) {
		if got := redactSensitiveConfigKeys(nil); got != nil {
			t.Errorf("expected nil, got %v", got)
		}
	})

	t.Run("redacts each sensitive keyword (case-insensitive, substring)", func(t *testing.T) {
		in := map[string]any{
			"DATABASE_PASSWORD": "hunter2", // "password"
			"api_key":           "abc",     // "api_key"
			"access_key_id":     "key-id",  // "access_key"
			"Session_Token":     "t",       // "token"
			"client_secret":     "s",       // "secret"
			"deploy_key":        "dk",      // "deploy_key"
			"db_password_hash":  "h",       // substring "password"
		}
		out := redactSensitiveConfigKeys(in)
		for k := range in {
			if out[k] != "[REDACTED]" {
				t.Errorf("key %q = %v, want [REDACTED]", k, out[k])
			}
		}
	})

	t.Run("non-sensitive values pass through unchanged", func(t *testing.T) {
		in := map[string]any{"host": "localhost", "port": 3306, "enabled": true}
		out := redactSensitiveConfigKeys(in)
		if out["host"] != "localhost" || out["port"] != 3306 || out["enabled"] != true {
			t.Errorf("non-sensitive values altered: %v", out)
		}
	})

	t.Run("nested maps are redacted recursively", func(t *testing.T) {
		in := map[string]any{
			"database": map[string]any{"password": "secret", "host": "db"},
		}
		out := redactSensitiveConfigKeys(in)
		nested, ok := out["database"].(map[string]any)
		if !ok {
			t.Fatalf("nested map missing or wrong type: %v", out["database"])
		}
		if nested["password"] != "[REDACTED]" {
			t.Errorf("nested password = %v, want [REDACTED]", nested["password"])
		}
		if nested["host"] != "db" {
			t.Errorf("nested host = %v, want db", nested["host"])
		}
	})
}
