//go:build windows

package helpers

import (
	"syscall"
	"unsafe"
)

// DiskUsage holds disk space information.
type DiskUsage struct {
	Total     uint64
	Available uint64
}

// GetDiskUsage returns disk space information for the given path.
func GetDiskUsage(path string) (*DiskUsage, error) {
	kernel32 := syscall.NewLazyDLL("kernel32.dll")
	getDiskFreeSpaceEx := kernel32.NewProc("GetDiskFreeSpaceExW")

	var freeBytesAvailable, totalBytes, totalFreeBytes uint64
	pathPtr, err := syscall.UTF16PtrFromString(path)
	if err != nil {
		return nil, err
	}

	ret, _, err := getDiskFreeSpaceEx.Call(
		uintptr(unsafe.Pointer(pathPtr)),             //nolint:gosec // G103: unsafe required for Windows syscall
		uintptr(unsafe.Pointer(&freeBytesAvailable)), //nolint:gosec // G103: unsafe required for Windows syscall
		uintptr(unsafe.Pointer(&totalBytes)),         //nolint:gosec // G103: unsafe required for Windows syscall
		uintptr(unsafe.Pointer(&totalFreeBytes)),     //nolint:gosec // G103: unsafe required for Windows syscall
	)
	if ret == 0 {
		return nil, err
	}

	return &DiskUsage{
		Total:     totalBytes,
		Available: freeBytesAvailable,
	}, nil
}
