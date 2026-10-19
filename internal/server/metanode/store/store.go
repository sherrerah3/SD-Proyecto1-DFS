package store

import "errors"

var ErrDataNodeNotFound = errors.New("datanode no encontrado")

type Store interface {
	// Archivos
	CreateFile(f FileMetadata) error
	GetFile(owner, filePath string) (FileMetadata, error)
	UpdateFile(f FileMetadata) error

	// Sustituye la metadata del archivo de forma atómica.
	// Devuelve la metadata anterior para liberar sus bloques tras el commit.
	ReplaceFileWithStaged(
		owner, targetPath, stagingPath string,
	) (FileMetadata, error)

	DeleteFile(owner, filePath string) error
	ListFiles(owner, dirPath string) ([]FileMetadata, error)

	// Bloques
	SaveBlockLocation(b BlockMetadata) error
	GetBlockLocation(blockID string) (BlockMetadata, error)
	DeleteBlockLocation(blockID string) error
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
	ReserveQuota(
		username string,
		deltaBytes, maxBytes int64,
	) error
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
