//go:build windows

package service

import (
	"fmt"
	"os"
	"syscall"
	"unsafe"
)

var serviceMoveFileExW = syscall.NewLazyDLL("kernel32.dll").NewProc("MoveFileExW")

const (
	serviceMoveFileReplaceExisting = 0x1
	serviceMoveFileWriteThrough    = 0x8
)

func replaceFile(src, dst string) error {
	source, err := syscall.UTF16PtrFromString(src)
	if err != nil {
		return err
	}
	destination, err := syscall.UTF16PtrFromString(dst)
	if err != nil {
		return err
	}
	result, _, callErr := serviceMoveFileExW.Call(
		uintptr(unsafe.Pointer(source)),
		uintptr(unsafe.Pointer(destination)),
		uintptr(serviceMoveFileReplaceExisting|serviceMoveFileWriteThrough),
	)
	if result == 0 {
		if callErr == syscall.Errno(0) {
			return fmt.Errorf("MoveFileExW failed without an operating system error")
		}
		return os.NewSyscallError("MoveFileExW", callErr)
	}
	return nil
}
