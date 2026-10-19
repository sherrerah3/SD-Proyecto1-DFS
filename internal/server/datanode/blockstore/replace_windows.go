//go:build windows

package blockstore

import (
	"fmt"
	"os"
	"syscall"
	"unsafe"
)

var blockMoveFileExW = syscall.NewLazyDLL("kernel32.dll").NewProc("MoveFileExW")

const (
	blockMoveFileReplaceExisting = 0x1
	blockMoveFileWriteThrough    = 0x8
)

// replaceFile atomically commits a temporary block file, replacing an older
// copy when a retry writes the same block ID. os.Rename does not replace an
// existing file consistently on Windows.
func replaceFile(src, dst string) error {
	source, err := syscall.UTF16PtrFromString(src)
	if err != nil {
		return err
	}
	destination, err := syscall.UTF16PtrFromString(dst)
	if err != nil {
		return err
	}
	result, _, callErr := blockMoveFileExW.Call(
		uintptr(unsafe.Pointer(source)),
		uintptr(unsafe.Pointer(destination)),
		uintptr(blockMoveFileReplaceExisting|blockMoveFileWriteThrough),
	)
	if result == 0 {
		if callErr == syscall.Errno(0) {
			return fmt.Errorf("MoveFileExW failed without an operating system error")
		}
		return os.NewSyscallError("MoveFileExW", callErr)
	}
	return nil
}
