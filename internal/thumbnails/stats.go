package thumbnails

// GetStats returns current statistics
func (m *Module) GetStats() Stats {
	m.statsMu.RLock()
	defer m.statsMu.RUnlock()
	return m.stats
}
