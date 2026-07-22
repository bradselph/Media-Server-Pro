package media

import "fmt"

// GetMatureState returns a detached snapshot of the persisted maturity fields.
// It is used to compensate a cross-module moderation commit if the scanner's
// second durable write fails.
func (m *Module) GetMatureState(path string) (isMature bool, score float64, reasons []string, err error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	meta := m.metadata[path]
	if meta == nil {
		item := m.media[path]
		if item == nil {
			return false, 0, nil, fmt.Errorf("media metadata not found: %s", path)
		}
		return item.IsMature, item.MatureScore, nil, nil
	}
	return meta.IsMature, meta.MatureScore, append([]string(nil), meta.MatureReasons...), nil
}
