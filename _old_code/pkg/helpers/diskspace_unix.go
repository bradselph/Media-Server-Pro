//go:build !windows

package helpers

import "syscall"

// DiskUsage holds disk space information.
type DiskUsage struct {
	Total     uint64
	Available uint64
}

// GetDiskUsage returns disk space information for the given path.
func GetDiskUsage(path string) (*DiskUsage, error) {
	var stat syscall.Statfs_t
	if err := syscall.Statfs(path, &stat); err != nil {
		return nil, err
	}
	return &DiskUsage{
		Total:     stat.Blocks * uint64(stat.Bsize),
		Available: stat.Bavail * uint64(stat.Bsize),
	}, nil
}
