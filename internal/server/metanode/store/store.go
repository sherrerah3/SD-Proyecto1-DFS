package store

import "errors"

// ErrDataNodeNotFound distingue "no existe" de otros fallos del store
var ErrDataNodeNotFound = errors.New("datanode no encontrado")

// Store define el contrato de persistencia de metadatos, seguro para uso concurrente.
type Store interface {
	// Archivos
	CreateFile(f FileMetadata) error
	GetFile(path string) (FileMetadata, error)
	UpdateFile(f FileMetadata) error
	DeleteFile(path string) error
	ListFiles(dirPath string) ([]FileMetadata, error)

	// Bloques
	SaveBlockLocation(b BlockMetadata) error
	GetBlockLocation(blockID string) (BlockMetadata, error)

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

	// Directorios
	// Directorios
	CreateDirectory(username, dirPath string) error
	DeleteDirectory(username, dirPath string) error
	DirectoryExists(username, dirPath string) bool
	ListDirectories(username, dirPath string) ([]string, error)
}
