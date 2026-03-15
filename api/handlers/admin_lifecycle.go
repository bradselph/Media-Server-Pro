package handlers

import (
	"os"
	"os/exec"
	"path/filepath"
	"time"

	"github.com/gin-gonic/gin"
)

// RestartServer initiates a server restart via self-exec.
// Calls the graceful shutdown callback first to drain in-flight requests and stop modules (P1-9).
func (h *Handler) RestartServer(c *gin.Context) {
	h.log.Warn("Server restart requested by admin")
	h.logAdminAction(c, &adminLogActionParams{Action: "restart_server", Target: "initiated"})

	writeSuccess(c, map[string]interface{}{
		"message": "Server restart initiated. The server will restart in a few seconds.",
		"status":  "restarting",
	})

	go func() {
		time.Sleep(1 * time.Second)

		// Drain connections and stop modules before exiting.
		h.log.Info("Running graceful shutdown before restart...")
		h.shutdownFunc()

		if os.Getenv("INVOCATION_ID") != "" {
			h.log.Info("Running under systemd — exiting with code 1 for service manager restart")
			os.Exit(1)
			return
		}

		h.log.Info("Initiating server restart via self-exec...")

		exe, err := os.Executable()
		if err != nil {
			h.log.Error("Failed to resolve executable path for restart: %v — falling back to exit", err)
			os.Exit(0)
			return
		}

		exe, err = filepath.EvalSymlinks(exe)
		if err != nil {
			h.log.Error("Failed to evaluate symlinks for restart: %v — falling back to exit", err)
			os.Exit(0)
			return
		}

		cmd := exec.Command(exe, os.Args[1:]...)
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		setCmdRestartAttrs(cmd) // detach child from parent session (platform-specific)

		if err := cmd.Start(); err != nil {
			h.log.Error("Failed to start replacement process: %v — falling back to exit", err)
			os.Exit(1)
			return
		}

		h.log.Info("Replacement process started (PID %d), exiting current instance", cmd.Process.Pid)
		os.Exit(0)
	}()
}

// ShutdownServer initiates a graceful server shutdown.
// Calls the shutdown callback to drain in-flight requests and stop modules before exiting (P1-9).
func (h *Handler) ShutdownServer(c *gin.Context) {
	h.log.Warn("Server shutdown requested by admin")
	h.logAdminAction(c, &adminLogActionParams{Action: "shutdown_server", Target: "initiated"})

	writeSuccess(c, map[string]interface{}{
		"message": "Server shutdown initiated. The server will shut down in a few seconds.",
		"status":  "shutting_down",
	})

	go func() {
		time.Sleep(1 * time.Second)
		h.log.Info("Running graceful shutdown...")
		h.shutdownFunc()
		os.Exit(0)
	}()
}
