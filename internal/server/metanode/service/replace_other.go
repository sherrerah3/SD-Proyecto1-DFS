//go:build !windows

package service

import "os"

func replaceFile(src, dst string) error {
	return os.Rename(src, dst)
}
