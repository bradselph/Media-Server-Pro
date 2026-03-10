package config

import (
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
		return strings.ToLower(val) == "true" || val == "1", true
	}
	return false, false
}

func envGetInt(keys ...string) (int, bool) {
	if val := envGetStr(keys...); val != "" {
		if i, err := strconv.Atoi(val); err == nil {
			return i, true
		}
	}
	return 0, false
}

func envGetInt64(keys ...string) (int64, bool) {
	if val := envGetStr(keys...); val != "" {
		if i, err := strconv.ParseInt(val, 10, 64); err == nil {
			return i, true
		}
	}
	return 0, false
}

func envGetFloat64(keys ...string) (float64, bool) {
	if val := envGetStr(keys...); val != "" {
		if f, err := strconv.ParseFloat(val, 64); err == nil {
			return f, true
		}
	}
	return 0, false
}

func envGetDuration(unit time.Duration, keys ...string) (time.Duration, bool) {
	if val := envGetStr(keys...); val != "" {
		if i, err := strconv.Atoi(val); err == nil {
			return time.Duration(i) * unit, true
		}
	}
	return 0, false
}

func envGetDurationString(keys ...string) (time.Duration, bool) {
	if val := envGetStr(keys...); val != "" {
		if d, err := time.ParseDuration(val); err == nil {
			return d, true
		}
	}
	return 0, false
}
