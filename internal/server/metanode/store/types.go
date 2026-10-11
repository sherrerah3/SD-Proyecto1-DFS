package store

import "time"

type FileMetadata struct {
	Name      string
	Path      string
	Owner     string
	SizeBytes int64
	SHA256    string
	BlockIDs  []string // orden importa: así se reconstruye el archivo
}

type BlockMetadata struct {
	BlockID   string
	DataNodes []string // IDs de los DataNodes que tienen una réplica
	SHA256    string
}

type DataNodeInfo struct {
	ID            string
	Address       string
	LastHeartbeat time.Time
	FreeBytes     int64
	Alive         bool
}

type UserInfo struct {
	Username       string
	PasswordHash   string
	QuotaUsedBytes int64
}
