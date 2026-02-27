//go:build windows

package server

import (
	"os"
	"os/signal"
)

// handleSignals handles OS signals for graceful shutdown (Windows version)
func (s *Server) handleSignals() {
	sigCh := make(chan os.Signal, 1)
	// Windows only supports os.Interrupt (Ctrl+C)
	signal.Notify(sigCh, os.Interrupt)

	sig := <-sigCh
	s.log.Info("Received signal: %v", sig)
	s.Shutdown()
}
