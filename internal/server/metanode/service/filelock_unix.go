//go:build !windows

package service

import (
	"errors"
	"os"
	"syscall"
)

// tryPlatformFileLock uses flock on the persistent shared lock file. The file must not
// be replaced/renamed while locked; only its contents are updated by the owner.
func tryPlatformFileLock(path string) (*heldFileLock, bool, error) {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o644)
	if err != nil {
		return nil, false, err
	}
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		_ = f.Close()
		if errors.Is(err, syscall.EWOULDBLOCK) || errors.Is(err, syscall.EAGAIN) {
			return nil, false, nil
		}
		return nil, false, err
	}
	return &heldFileLock{
		file: f,
		release: func() error {
			unlockErr := syscall.Flock(int(f.Fd()), syscall.LOCK_UN)
			closeErr := f.Close()
			if unlockErr != nil {
				return unlockErr
			}
			return closeErr
		},
	}, true, nil
}
