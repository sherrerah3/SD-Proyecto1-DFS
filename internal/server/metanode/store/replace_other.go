//go:build !windows

package store

import "os"

// replaceFile atomically replaces dst with src on platforms where os.Rename
// supports replacement within the same directory.
func replaceFile(src, dst string) error {
	return os.Rename(src, dst)
}
