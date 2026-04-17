package claude

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"time"

	"media-server-pro/internal/config"
)

// Tool is the server-side counterpart of an Anthropic tool definition. Each
// Tool exposes a JSON schema (for Claude) and an Execute method (for the
// server). Mutating tools report IsWrite()=true so the module can gate them.
type Tool interface {
	Name() string
	Description() string
	InputSchema() map[string]any
	IsWrite() bool
	Execute(ctx context.Context, input json.RawMessage, rc *RunContext) (string, error)
}

// RunContext is passed to every tool execution. It carries the slice of config
// and identity bits a tool needs without handing over the whole Module.
type RunContext struct {
	Cfg      config.ClaudeConfig
	UserID   string
	Username string
	IP       string
}

// rateLimiter is a simple per-user sliding-window limiter keyed by (userID).
type rateLimiter struct {
	mu      sync.Mutex
	buckets map[string][]time.Time
}

func newRateLimiter() *rateLimiter {
	return &rateLimiter{buckets: make(map[string][]time.Time)}
}

// allow reports whether this user has budget for another chat turn under limit
// (per minute). Zero limit disables the check.
func (r *rateLimiter) allow(userID string, limit int) bool {
	if limit <= 0 {
		return true
	}
	now := time.Now()
	cutoff := now.Add(-time.Minute)
	r.mu.Lock()
	defer r.mu.Unlock()
	bucket := r.buckets[userID]
	fresh := bucket[:0]
	for _, t := range bucket {
		if t.After(cutoff) {
			fresh = append(fresh, t)
		}
	}
	if len(fresh) >= limit {
		r.buckets[userID] = fresh
		return false
	}
	fresh = append(fresh, now)
	r.buckets[userID] = fresh
	return true
}

// validateAllowedPath returns an error if target is not contained within one
// of the configured AllowedPaths prefixes. An empty AllowedPaths list denies
// all paths.
func validateAllowedPath(target string, allowed []string) error {
	target = strings.TrimSpace(target)
	if target == "" {
		return errors.New("path is empty")
	}
	if len(allowed) == 0 {
		return errors.New("no allowed paths are configured")
	}
	for _, p := range allowed {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		// Prefix match — operators are expected to write absolute paths or
		// explicit relative prefixes (e.g. "./logs"). We intentionally do NOT
		// resolve symlinks here; the OS will enforce final permission.
		if target == p || strings.HasPrefix(target, strings.TrimRight(p, "/")+"/") {
			return nil
		}
	}
	return errors.New("path is outside allowed paths")
}

// validateAllowedCommand returns an error if cmd is not exactly in allowed.
func validateAllowedCommand(cmd string, allowed []string) error {
	cmd = strings.TrimSpace(cmd)
	if cmd == "" {
		return errors.New("command is empty")
	}
	if len(allowed) == 0 {
		return errors.New("no allowed shell commands are configured")
	}
	for _, a := range allowed {
		if strings.TrimSpace(a) == cmd {
			return nil
		}
	}
	return errors.New("command is not in the allowlist")
}

// validateAllowedService returns an error if svc is not in allowed.
func validateAllowedService(svc string, allowed []string) error {
	svc = strings.TrimSpace(svc)
	if svc == "" {
		return errors.New("service is empty")
	}
	if len(allowed) == 0 {
		return errors.New("no allowed services are configured")
	}
	for _, a := range allowed {
		if strings.TrimSpace(a) == svc {
			return nil
		}
	}
	return errors.New("service is not in the allowlist")
}
