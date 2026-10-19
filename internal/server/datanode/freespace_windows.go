//go:build windows

package datanode

import (
	"path/filepath"
	"syscall"
	"unsafe"
)

var getDiskFreeSpaceExW = syscall.NewLazyDLL("kernel32.dll").NewProc("GetDiskFreeSpaceExW")

// freeBytes returns the amount of disk space available to the current user on
// the volume containing dir. It returns zero if Windows cannot query the path.
func freeBytes(dir string) int64 {
	absPath, err := filepath.Abs(dir)
	if err != nil {
		return 0
	}
	path, err := syscall.UTF16PtrFromString(absPath)
	if err != nil {
		return 0
	}
	var available uint64
	result, _, _ := getDiskFreeSpaceExW.Call(
		uintptr(unsafe.Pointer(path)),
		uintptr(unsafe.Pointer(&available)),
		0,
		0,
	)
	if result == 0 {
		return 0
	}
	const maxInt64 = uint64(1<<63 - 1)
	if available > maxInt64 {
		return int64(maxInt64)
	}
	return int64(available)
}
