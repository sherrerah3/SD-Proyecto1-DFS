//go:build !windows

package blockstore

import "os"

func replaceFile(src, dst string) error {
	return os.Rename(src, dst)
}
