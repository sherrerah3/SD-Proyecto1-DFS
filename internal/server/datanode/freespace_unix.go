//go:build linux || darwin

package datanode

import "golang.org/x/sys/unix"

// freeBytes devuelve el espacio libre del disco que contiene dir
func freeBytes(dir string) int64 {
	var st unix.Statfs_t
	if err := unix.Statfs(dir, &st); err != nil {
		return 0
	}
	return int64(st.Bavail) * int64(st.Bsize)
}
