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
		uintptr(unsafe.Pointer(pathPtr)),
		uintptr(unsafe.Pointer(&freeBytesAvailable)),
		uintptr(unsafe.Pointer(&totalBytes)),
		uintptr(unsafe.Pointer(&totalFreeBytes)),
	)
	if ret == 0 {
		return nil, err
	}

	return &DiskUsage{
		Total:     totalBytes,
		Available: freeBytesAvailable,
	}, nil
}
