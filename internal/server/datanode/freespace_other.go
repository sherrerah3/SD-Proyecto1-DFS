//go:build !linux && !darwin && !windows

package datanode

// freeBytes returns zero on platforms without a supported filesystem API.
func freeBytes(dir string) int64 {
	return 0
}
