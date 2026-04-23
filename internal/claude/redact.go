package claude

import (
	"regexp"
	"strings"
)

// redactor scrubs likely-secret content from strings before they're sent to
// the Anthropic API or persisted. The set is intentionally conservative — the
// goal is belt-and-suspenders protection layered over config-level secret
// handling, not a perfect DLP system.
var redactionPatterns = []*regexp.Regexp{
	// Long, quoted tokens assigned to key-looking names.
	regexp.MustCompile(`(?i)(api[_-]?key|secret|password|token|bearer|authorization)(\s*[:=]\s*|\s+is\s+)(["']?)[^"'\s]{8,}(["']?)`),
	// Bearer tokens in headers.
	regexp.MustCompile(`(?i)bearer\s+[A-Za-z0-9\-\._~\+\/]{16,}={0,2}`),
	// MySQL DSN passwords: user:password@tcp(...)
	regexp.MustCompile(`([a-zA-Z0-9_\-]+):[^@\s/]+@tcp\(`),
	// Generic "password":"..." JSON fragments.
	regexp.MustCompile(`(?i)"(password|api_key|secret_access_key|access_key_id|github_token|cookie|session_id)"\s*:\s*"[^"]+"`),
	// Anthropic/OpenAI-style keys.
	regexp.MustCompile(`sk-ant-[A-Za-z0-9\-_]{20,}`),
	regexp.MustCompile(`sk-[A-Za-z0-9]{20,}`),
}

// redact applies every pattern to s and returns a sanitized copy. Original
// structure is preserved so Claude can still reason about shape/position.
func redact(s string) string {
	if s == "" {
		return s
	}
	out := s
	for _, re := range redactionPatterns {
		out = re.ReplaceAllStringFunc(out, func(match string) string {
			// Keep the prefix up to the value, replace the value with a placeholder.
			if strings.Contains(strings.ToLower(match), "@tcp(") {
				return strings.Split(match, ":")[0] + ":[REDACTED]@tcp("
			}
			if strings.HasPrefix(strings.ToLower(match), "bearer ") {
				return "Bearer [REDACTED]"
			}
			if strings.HasPrefix(match, "sk-") {
				return "[REDACTED_API_KEY]"
			}
			return "[REDACTED]"
		})
	}
	return out
}

// redactMap walks a map[string]any shallowly and redacts string values for
// keys whose names suggest secrets. Used when serializing config snapshots.
var secretKeys = map[string]struct{}{
	"api_key":           {},
	"apikey":            {},
	"secret":            {},
	"password":          {},
	"password_hash":     {},
	"token":             {},
	"github_token":      {},
	"access_key_id":     {},
	"secret_access_key": {},
	"session_id":        {},
	"cookie":            {},
	"authorization":     {},
}

func redactSlice(in []any) []any {
	out := make([]any, len(in))
	for i, v := range in {
		switch child := v.(type) {
		case map[string]any:
			out[i] = redactMap(child)
		case []any:
			out[i] = redactSlice(child)
		case string:
			out[i] = redact(child)
		default:
			out[i] = v
		}
	}
	return out
}

func redactMap(in map[string]any) map[string]any {
	if in == nil {
		return nil
	}
	out := make(map[string]any, len(in))
	for k, v := range in {
		lk := strings.ToLower(k)
		if _, sensitive := secretKeys[lk]; sensitive {
			if s, ok := v.(string); ok && s != "" {
				out[k] = "[REDACTED]"
				continue
			}
		}
		switch child := v.(type) {
		case map[string]any:
			out[k] = redactMap(child)
		case []any:
			out[k] = redactSlice(child)
		case string:
			out[k] = redact(child)
		default:
			out[k] = v
		}
	}
	return out
}
