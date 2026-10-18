package store

import "errors"

// ErrDataNodeNotFound distingue "no existe" de otros fallos del store
var ErrDataNodeNotFound = errors.New("datanode no encontrado")

// Store define el contrato de persistencia de metadatos, seguro para uso concurrente.
type Store interface {
	// Archivos (por dueño)
	CreateFile(f FileMetadata) error
	GetFile(owner, filePath string) (FileMetadata, error)
	UpdateFile(f FileMetadata) error
	// ModifyFile aplica fn de forma atómica y devuelve el valor previo
	ModifyFile(owner, filePath string, fn func(*FileMetadata) error) (FileMetadata, error)
	DeleteFile(owner, filePath string) error
	ListFiles(owner, dirPath string) ([]FileMetadata, error)

	// Bloques
	SaveBlockLocation(b BlockMetadata) error
	GetBlockLocation(blockID string) (BlockMetadata, error)
	DeleteBlockLocation(blockID string) error
	// ReportBlock registra que dataNodeID almacenó el bloque
	ReportBlock(blockID, dataNodeID, hash string) error

	// DataNodes
	RegisterDataNode(d DataNodeInfo) error
	UpdateHeartbeat(id string, freeBytes int64) error
	ListAliveDataNodes() ([]DataNodeInfo, error)
	ListDataNodes() ([]DataNodeInfo, error)
	SetDataNodeAlive(id string, alive bool) error

	// Usuarios
	CreateUser(u UserInfo) error
	GetUser(username string) (UserInfo, error)
	UpdateQuotaUsed(username string, deltaBytes int64) error
	// ReserveQuota suma el delta solo si no supera maxBytes
	ReserveQuota(username string, deltaBytes, maxBytes int64) error
	CountUsers() int

	// Directorios
	CreateDirectory(username, dirPath string) error
	DeleteDirectory(username, dirPath string) error
	DirectoryExists(username, dirPath string) bool
	ListDirectories(username, dirPath string) ([]string, error)

	// Borrados pendientes por DataNode
	EnqueueBlockDeletion(dataNodeID, blockID string) error
	DequeueBlockDeletions(dataNodeID string) ([]string, error)
}
