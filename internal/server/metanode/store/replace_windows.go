//go:build windows

package store

import (
	"fmt"
	"os"
	"syscall"
	"unsafe"
)

var moveFileExW = syscall.NewLazyDLL("kernel32.dll").NewProc("MoveFileExW")

const (
	moveFileReplaceExisting = 0x1
	moveFileWriteThrough    = 0x8
)

// replaceFile uses MoveFileExW with replacement semantics. os.Rename on
// Windows does not reliably replace an existing destination file, which
// would otherwise make all state writes after the first one fail.
func replaceFile(src, dst string) error {
	source, err := syscall.UTF16PtrFromString(src)
	if err != nil {
		return err
	}
	destination, err := syscall.UTF16PtrFromString(dst)
	if err != nil {
		return err
	}

	result, _, callErr := moveFileExW.Call(
		uintptr(unsafe.Pointer(source)),
		uintptr(unsafe.Pointer(destination)),
		uintptr(moveFileReplaceExisting|moveFileWriteThrough),
	)
	if result == 0 {
		if callErr == syscall.Errno(0) {
			return fmt.Errorf("MoveFileExW failed without an operating system error")
		}
		return os.NewSyscallError("MoveFileExW", callErr)
	}
	return nil
}
