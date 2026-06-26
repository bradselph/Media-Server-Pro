package runtimeenv

import (
	"runtime"

	"media-server-pro/internal/logger"
)

// UsableCPUs reports how many CPUs the Go runtime will actually schedule
// goroutines across. It returns GOMAXPROCS rather than the raw logical CPU
// count because, as of Go 1.25, the runtime automatically lowers GOMAXPROCS to
// match a cgroup CPU quota when the process runs in a CPU-limited container.
// Sizing CPU-bound worker pools off this value therefore fills a bare-metal
// host without oversubscribing a throttled container.
//
// The result is always at least 1.
func UsableCPUs() int {
	if n := runtime.GOMAXPROCS(0); n >= 1 {
		return n
	}
	return 1
}

// LogCPUProfile records, once at startup, how many CPUs the process may use
// versus how many the host physically has, so an operator can confirm the
// server is not being silently throttled below the hardware. GOMAXPROCS being
// lower than the logical count almost always means a container/cgroup CPU quota.
func LogCPUProfile(log *logger.Logger) {
	usable := UsableCPUs()
	logical := runtime.NumCPU()
	if usable < logical {
		log.Info("CPU: scheduling across %d of %d logical CPUs (GOMAXPROCS limited — likely a container/cgroup CPU quota)", usable, logical)
		return
	}
	log.Info("CPU: scheduling across all %d logical CPUs (GOMAXPROCS=%d)", logical, usable)
}
