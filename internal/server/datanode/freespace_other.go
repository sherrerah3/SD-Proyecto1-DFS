//go:build !linux && !darwin

package datanode

// freeBytes devuelve 0 donde no hay statfs (solo para compilar)
func freeBytes(dir string) int64 {
	return 0
}
