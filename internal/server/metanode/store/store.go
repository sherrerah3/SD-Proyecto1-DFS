package store

// Store define el contrato de persistencia de metadatos, seguro para uso concurrente.
type Store interface {
	// Archivos
	CreateFile(f FileMetadata) error
	GetFile(path string) (FileMetadata, error)
	DeleteFile(path string) error
	ListFiles(dirPath string) ([]FileMetadata, error)

	// Bloques
	SaveBlockLocation(b BlockMetadata) error
	GetBlockLocation(blockID string) (BlockMetadata, error)

	// DataNodes
	RegisterDataNode(d DataNodeInfo) error
	UpdateHeartbeat(id string, freeBytes int64) error
	ListAliveDataNodes() ([]DataNodeInfo, error)

	// Usuarios
	CreateUser(u UserInfo) error
	GetUser(username string) (UserInfo, error)
	UpdateQuotaUsed(username string, deltaBytes int64) error
}
