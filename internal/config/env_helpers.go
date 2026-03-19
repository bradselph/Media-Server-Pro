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

func envGetBool(keys ...string) (bool, bool) {
	if val := envGetStr(keys...); val != "" {
		lower := strings.ToLower(val)
		if lower != "true" && lower != "false" && val != "1" && val != "0" {
			fmt.Fprintf(os.Stderr, "Warning: env var %s has invalid boolean value %q, treating as false\n", keys[0], val)
		}
		return lower == "true" || val == "1", true
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
		i, err := strconv.Atoi(val)
		if err == nil {
			return time.Duration(i) * unit, true
		}
		fmt.Fprintf(os.Stderr, "Warning: env var %s has invalid duration value %q: %v\n", keys[0], val, err)
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
