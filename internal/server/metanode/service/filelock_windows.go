//go:build windows

package service

import (
	"errors"
	"os"

	"golang.org/x/sys/windows"
)

// tryPlatformFileLock uses a Windows exclusive byte-range lock on the shared lock file.
func tryPlatformFileLock(path string) (*heldFileLock, bool, error) {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o644)
	if err != nil {
		return nil, false, err
	}
	var overlapped windows.Overlapped
	flags := uint32(windows.LOCKFILE_EXCLUSIVE_LOCK | windows.LOCKFILE_FAIL_IMMEDIATELY)
	err = windows.LockFileEx(windows.Handle(f.Fd()), flags, 0, 1, 0, &overlapped)
	if err != nil {
		_ = f.Close()
		if errors.Is(err, windows.ERROR_LOCK_VIOLATION) {
			return nil, false, nil
		}
		return nil, false, err
	}
	return &heldFileLock{
		file: f,
		release: func() error {
			unlockErr := windows.UnlockFileEx(windows.Handle(f.Fd()), 0, 1, 0, &overlapped)
			closeErr := f.Close()
			if unlockErr != nil {
				return unlockErr
			}
			return closeErr
		},
	}, true, nil
}
