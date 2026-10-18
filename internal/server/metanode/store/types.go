package store

import "time"

type FileMetadata struct {
	Name      string
	Path      string
	Owner     string
	SizeBytes int64
	SHA256    string
	BlockIDs  []string        // orden importa: así se reconstruye el archivo
	Pending   *PendingVersion // reemplazo en subida; la versión actual sigue legible
}

// PendingVersion es la versión nueva de un archivo que aún no se confirma.
type PendingVersion struct {
	SizeBytes     int64
	BlockIDs      []string
	ReservedBytes int64 // cuota reservada al iniciar el reemplazo
}

type BlockMetadata struct {
	BlockID       string
	DataNodes     []string          // IDs de los DataNodes a los que se asignó una réplica
	SHA256        string            // hash del bloque, fijado cuando se alcanza el quórum
	Confirmations map[string]string // datanodeID -> hash reportado
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
