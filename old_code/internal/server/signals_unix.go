//go:build !windows

package server

import (
	"os"
	"os/signal"
	"syscall"
)

// handleSignals handles OS signals for graceful shutdown (Unix version)
func (s *Server) handleSignals() {
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	sig := <-sigCh
	s.log.Info("Received signal: %v", sig)
	s.Shutdown()
}
