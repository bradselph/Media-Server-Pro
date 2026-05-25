// Package runtimeenv applies process-level Go runtime tuning derived from the
// host environment and admin configuration. It is intentionally tiny and
// dependency-light so it can be called very early in startup.
package runtimeenv

import (
	"bytes"
	"os"
	"runtime/debug"
	"strconv"

	"media-server-pro/internal/logger"
)

// defaultMemoryLimitPercent is used when the admin leaves the knob at 0 (auto).
// 75% of RAM leaves headroom for the OS page cache (which itself speeds up
// media file reads), the database, and any ffmpeg child processes.
const defaultMemoryLimitPercent = 75

// tunedGCPercent raises the GC growth target when an auto memory limit is in
// effect, so the heap is allowed to grow and the collector runs less often
// (less CPU spent on GC) — the soft memory limit is the real ceiling. Skipped
// when GOGC is set in the environment.
const tunedGCPercent = 200

// TuneMemoryLimit sets the Go runtime soft memory limit (the programmatic
// equivalent of GOMEMLIMIT) to a percentage of total system RAM, so a large
// host actually uses its RAM as GC headroom instead of collecting at ~2x a
// tiny live heap and handing memory straight back to the OS.
//
// The limit is SOFT: as live usage approaches it the GC becomes more
// aggressive, so this cannot by itself cause an out-of-memory kill.
//
// It is a no-op (leaving Go's defaults untouched) when:
//   - GOMEMLIMIT is already set in the environment (the operator/orchestrator
//     owns the limit in that case),
//   - total system RAM cannot be determined (e.g. non-Linux dev hosts),
//   - percent resolves outside the sane range.
//
// Safe to call repeatedly (e.g. from a config-change watcher).
func TuneMemoryLimit(percent int, log *logger.Logger) {
	if v := os.Getenv("GOMEMLIMIT"); v != "" {
		log.Info("GOMEMLIMIT=%s set in environment; leaving Go memory limit unmanaged", v)
		return
	}

	if percent == 0 {
		percent = defaultMemoryLimitPercent
	}
	if percent < 10 || percent > 95 {
		log.Warn("server.memory_limit_percent=%d is out of range [10,95]; using %d", percent, defaultMemoryLimitPercent)
		percent = defaultMemoryLimitPercent
	}

	total := totalSystemMemoryBytes()
	if total == 0 {
		log.Info("Could not determine total system memory; leaving Go memory limit at default")
		return
	}

	limit := int64(float64(total) * float64(percent) / 100.0)
	debug.SetMemoryLimit(limit)
	log.Info("Go soft memory limit set to %d MiB (%d%% of %d MiB total RAM)", limit>>20, percent, total>>20)

	// Let the heap grow toward the limit instead of collecting at 2x a small
	// live heap. Respect an operator-set GOGC.
	if os.Getenv("GOGC") == "" {
		debug.SetGCPercent(tunedGCPercent)
		log.Info("GC growth target set to %d%% (heap may grow before collection; soft memory limit is the ceiling)", tunedGCPercent)
	}
}

// totalSystemMemoryBytes returns total physical RAM in bytes, or 0 if it cannot
// be determined on this platform. Reads /proc/meminfo (Linux); other platforms
// return 0 so tuning is skipped.
func totalSystemMemoryBytes() uint64 {
	data, err := os.ReadFile("/proc/meminfo")
	if err != nil {
		return 0
	}
	for line := range bytes.SplitSeq(data, []byte{'\n'}) {
		if !bytes.HasPrefix(line, []byte("MemTotal:")) {
			continue
		}
		fields := bytes.Fields(line)
		if len(fields) < 2 {
			return 0
		}
		kb, err := strconv.ParseUint(string(fields[1]), 10, 64)
		if err != nil {
			return 0
		}
		return kb * 1024 // MemTotal is reported in kB
	}
	return 0
}
