package config

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
)

func envGetStr(keys ...string) string {
	for _, key := range keys {
		if val := os.Getenv(key); val != "" {
			return val
		}
	}
	return ""
}

func envGetBool(keys ...string) (value, found bool) {
	if val := envGetStr(keys...); val != "" {
		lower := strings.ToLower(val)
		switch lower {
		case "true", "1", "yes", "on":
			return true, true
		case "false", "0", "no", "off":
			return false, true
		default:
			fmt.Fprintf(os.Stderr, "Warning: env var %s has invalid boolean value %q, ignoring\n", keys[0], val)
			return false, false
		}
	}
	return false, false
}

func envGetInt(keys ...string) (int, bool) {
	if val := envGetStr(keys...); val != "" {
		i, err := strconv.Atoi(val)
		if err == nil {
			return i, true
		}
		fmt.Fprintf(os.Stderr, "Warning: env var %s has invalid integer value %q: %v\n", keys[0], val, err)
	}
	return 0, false
}

func envGetInt64(keys ...string) (int64, bool) {
	if val := envGetStr(keys...); val != "" {
		i, err := strconv.ParseInt(val, 10, 64)
		if err == nil {
			return i, true
		}
		fmt.Fprintf(os.Stderr, "Warning: env var %s has invalid int64 value %q: %v\n", keys[0], val, err)
	}
	return 0, false
}

func envGetFloat64(keys ...string) (float64, bool) {
	if val := envGetStr(keys...); val != "" {
		f, err := strconv.ParseFloat(val, 64)
		if err == nil {
			return f, true
		}
		fmt.Fprintf(os.Stderr, "Warning: env var %s has invalid float64 value %q: %v\n", keys[0], val, err)
	}
	return 0, false
}

func envGetDuration(unit time.Duration, keys ...string) (time.Duration, bool) {
	if val := envGetStr(keys...); val != "" {
		if i, err := strconv.Atoi(val); err == nil {
			return time.Duration(i) * unit, true
		}
		// Fall back to Go duration string (e.g. "30s", "1m30s")
		if d, err := time.ParseDuration(val); err == nil {
			return d, true
		}
		fmt.Fprintf(os.Stderr, "Warning: env var %s has invalid duration value %q (expected integer or duration string)\n", keys[0], val)
	}
	return 0, false
}

func envGetDurationString(keys ...string) (time.Duration, bool) {
	if val := envGetStr(keys...); val != "" {
		d, err := time.ParseDuration(val)
		if err == nil {
			return d, true
		}
		fmt.Fprintf(os.Stderr, "Warning: env var %s has invalid duration string %q: %v\n", keys[0], val, err)
	}
	return 0, false
}

// splitTrimmed splits s by "," and trims whitespace from each element.
// Empty elements after trimming are excluded from the result.
func splitTrimmed(s string) []string {
	parts := strings.Split(s, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if trimmed := strings.TrimSpace(p); trimmed != "" {
			out = append(out, trimmed)
		}
	}
	return out
}
