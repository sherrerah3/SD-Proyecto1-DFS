package store

import (
	"encoding/json"
	"errors"
	"fmt"
	"path"
	"strings"
	"sync"
	"time"
)

var _ Store = (*MemoryStore)(nil)

type MemoryStore struct {
	mu          sync.RWMutex
	files       map[string]FileMetadata
	blocks      map[string]BlockMetadata
	datanodes   map[string]DataNodeInfo
	users       map[string]UserInfo
	directories map[string]bool
	pendingDel  map[string][]string
}

func NewMemoryStore() *MemoryStore {
	return &MemoryStore{
		files:       make(map[string]FileMetadata),
		blocks:      make(map[string]BlockMetadata),
		datanodes:   make(map[string]DataNodeInfo),
		users:       make(map[string]UserInfo),
		directories: make(map[string]bool),
		pendingDel:  make(map[string][]string),
	}
}

// --- Archivos ---

func fileKey(owner, filePath string) string {
	return owner + "\x00" + filePath
}

func (s *MemoryStore) CreateFile(f FileMetadata) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	key := fileKey(f.Owner, f.Path)
	if _, exists := s.files[key]; exists {
		return ErrFileAlreadyExists
	}

	s.files[key] = f
	return nil
}

func (s *MemoryStore) GetFile(
	owner, filePath string,
) (FileMetadata, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	f, ok := s.files[fileKey(owner, filePath)]
	if !ok {
		return FileMetadata{}, fmt.Errorf(
			"archivo no encontrado: %s",
			filePath,
		)
	}

	return f, nil
}

func (s *MemoryStore) UpdateFile(f FileMetadata) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	key := fileKey(f.Owner, f.Path)
	if _, ok := s.files[key]; !ok {
		return fmt.Errorf("archivo no encontrado: %s", f.Path)
	}

	s.files[key] = f
	return nil
}

// ReplaceFileWithStaged publica la metadata temporal en la ruta definitiva
// bajo el mismo mutex. El archivo anterior se devuelve para limpiar sus bloques.
func (s *MemoryStore) ReplaceFileWithStaged(
	owner, targetPath, stagingPath string,
) (FileMetadata, error) {
	if targetPath == stagingPath {
		return FileMetadata{}, errors.New(
			"la ruta temporal y la ruta destino no pueden ser iguales",
		)
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	targetKey := fileKey(owner, targetPath)
	stagingKey := fileKey(owner, stagingPath)

	oldFile, targetExists := s.files[targetKey]
	stagedFile, stagingExists := s.files[stagingKey]

	if !targetExists {
		return FileMetadata{}, fmt.Errorf(
			"archivo destino no encontrado: %s",
			targetPath,
		)
	}

	if !stagingExists {
		return FileMetadata{}, fmt.Errorf(
			"archivo temporal no encontrado: %s",
			stagingPath,
		)
	}

	if stagedFile.SHA256 == "" {
		return FileMetadata{}, errors.New(
			"la versión temporal todavía no está completa",
		)
	}

	stagedFile.Owner = owner
	stagedFile.Path = targetPath
	stagedFile.Name = path.Base(targetPath)

	s.files[targetKey] = stagedFile
	delete(s.files, stagingKey)

	return oldFile, nil
}

func (s *MemoryStore) DeleteFile(
	owner, filePath string,
) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	key := fileKey(owner, filePath)
	if _, ok := s.files[key]; !ok {
		return fmt.Errorf("archivo no encontrado: %s", filePath)
	}

	delete(s.files, key)
	return nil
}

// ListFiles devuelve archivos del usuario bajo dirPath.
func (s *MemoryStore) ListFiles(
	owner, dirPath string,
) ([]FileMetadata, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	prefix := dirPath
	if !strings.HasSuffix(prefix, "/") {
		prefix += "/"
	}

	result := make([]FileMetadata, 0)

	for _, f := range s.files {
		if f.Owner == owner && strings.HasPrefix(f.Path, prefix) {
			result = append(result, f)
		}
	}

	return result, nil
}

// --- Bloques ---

func (s *MemoryStore) SaveBlockLocation(b BlockMetadata) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.blocks[b.BlockID] = b
	return nil
}

func (s *MemoryStore) GetBlockLocation(
	blockID string,
) (BlockMetadata, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	b, ok := s.blocks[blockID]
	if !ok {
		return BlockMetadata{}, fmt.Errorf(
			"bloque no encontrado: %s",
			blockID,
		)
	}

	return b, nil
}

func (s *MemoryStore) ReportBlock(
	blockID, dataNodeID, hash string,
) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	b, ok := s.blocks[blockID]
	if !ok {
		return fmt.Errorf("bloque no encontrado: %s", blockID)
	}

	if b.Confirmations == nil {
		b.Confirmations = make(map[string]string)
	}

	b.Confirmations[dataNodeID] = hash
	s.blocks[blockID] = b

	return nil
}

func (s *MemoryStore) DeleteBlockLocation(blockID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	delete(s.blocks, blockID)
	return nil
}

func (s *MemoryStore) EnqueueBlockDeletion(
	dataNodeID, blockID string,
) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.pendingDel[dataNodeID] = append(
		s.pendingDel[dataNodeID],
		blockID,
	)

	return nil
}

func (s *MemoryStore) DequeueBlockDeletions(
	dataNodeID string,
) ([]string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	ids := s.pendingDel[dataNodeID]
	delete(s.pendingDel, dataNodeID)

	return ids, nil
}

// --- DataNodes ---

func (s *MemoryStore) RegisterDataNode(d DataNodeInfo) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	d.Alive = true
	d.LastHeartbeat = time.Now()
	s.datanodes[d.ID] = d

	return nil
}

func (s *MemoryStore) UpdateHeartbeat(
	id string,
	freeBytes int64,
) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	d, ok := s.datanodes[id]
	if !ok {
		return ErrDataNodeNotFound
	}

	d.FreeBytes = freeBytes
	d.LastHeartbeat = time.Now()
	d.Alive = true
	s.datanodes[id] = d

	return nil
}

func (s *MemoryStore) ListAliveDataNodes() ([]DataNodeInfo, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	result := make([]DataNodeInfo, 0)

	for _, d := range s.datanodes {
		if d.Alive {
			result = append(result, d)
		}
	}

	return result, nil
}

func (s *MemoryStore) ListDataNodes() ([]DataNodeInfo, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	out := make([]DataNodeInfo, 0, len(s.datanodes))
	for _, d := range s.datanodes {
		out = append(out, d)
	}

	return out, nil
}

func (s *MemoryStore) SetDataNodeAlive(
	id string,
	alive bool,
) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	d, ok := s.datanodes[id]
	if !ok {
		return ErrDataNodeNotFound
	}

	d.Alive = alive
	s.datanodes[id] = d

	return nil
}

// --- Usuarios ---

func (s *MemoryStore) CreateUser(u UserInfo) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if _, exists := s.users[u.Username]; exists {
		return fmt.Errorf("el usuario %s ya existe", u.Username)
	}

	s.users[u.Username] = u
	return nil
}

func (s *MemoryStore) GetUser(username string) (UserInfo, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	u, ok := s.users[username]
	if !ok {
		return UserInfo{}, fmt.Errorf("usuario no encontrado: %s", username)
	}

	return u, nil
}

func (s *MemoryStore) UpdateQuotaUsed(
	username string,
	deltaBytes int64,
) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	u, ok := s.users[username]
	if !ok {
		return fmt.Errorf("usuario no encontrado: %s", username)
	}

	u.QuotaUsedBytes += deltaBytes
	s.users[username] = u

	return nil
}

func (s *MemoryStore) ReserveQuota(
	username string,
	deltaBytes, maxBytes int64,
) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	u, ok := s.users[username]
	if !ok {
		return fmt.Errorf("usuario no encontrado: %s", username)
	}

	if u.QuotaUsedBytes+deltaBytes > maxBytes {
		return ErrQuotaExceeded
	}

	u.QuotaUsedBytes += deltaBytes
	s.users[username] = u

	return nil
}

func (s *MemoryStore) CountUsers() int {
	s.mu.RLock()
	defer s.mu.RUnlock()

	return len(s.users)
}

// --- Snapshot para persistencia ---

type snapshot struct {
	Files       map[string]FileMetadata  `json:"files"`
	Blocks      map[string]BlockMetadata `json:"blocks"`
	Users       map[string]UserInfo      `json:"users"`
	Directories map[string]bool          `json:"directories"`
}

func (s *MemoryStore) Snapshot() ([]byte, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	return json.Marshal(snapshot{
		Files:       s.files,
		Blocks:      s.blocks,
		Users:       s.users,
		Directories: s.directories,
	})
}

func (s *MemoryStore) Restore(data []byte) error {
	var snap snapshot

	if err := json.Unmarshal(data, &snap); err != nil {
		return err
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	s.files = orEmpty(snap.Files)
	s.blocks = orEmpty(snap.Blocks)
	s.users = orEmpty(snap.Users)
	s.directories = orEmpty(snap.Directories)

	return nil
}

func orEmpty[K comparable, V any](m map[K]V) map[K]V {
	if m == nil {
		return make(map[K]V)
	}

	return m
}

var (
	ErrDirectoryNotFound = errors.New("directorio no encontrado")
	ErrDirectoryExists   = errors.New("el directorio ya existe")
	ErrDirectoryNotEmpty = errors.New("el directorio no está vacío")
	ErrQuotaExceeded     = errors.New("cuota de almacenamiento excedida")
	ErrFileAlreadyExists = errors.New("ya existe un archivo en esa ruta")
)

func directoryKey(username, dirPath string) string {
	return username + "\x00" + path.Clean(dirPath)
}

func (s *MemoryStore) DirectoryExists(
	username, dirPath string,
) bool {
	dirPath = path.Clean(dirPath)

	if dirPath == "/" {
		return true
	}

	s.mu.RLock()
	defer s.mu.RUnlock()

	return s.directories[directoryKey(username, dirPath)]
}

func (s *MemoryStore) CreateDirectory(
	username, dirPath string,
) error {
	dirPath = path.Clean(dirPath)

	if dirPath == "/" {
		return ErrDirectoryExists
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	key := directoryKey(username, dirPath)

	if s.directories[key] {
		return ErrDirectoryExists
	}

	parent := path.Dir(dirPath)
	if parent != "/" &&
		!s.directories[directoryKey(username, parent)] {
		return ErrDirectoryNotFound
	}

	s.directories[key] = true
	return nil
}

func (s *MemoryStore) ListDirectories(
	username, dirPath string,
) ([]string, error) {
	dirPath = path.Clean(dirPath)

	s.mu.RLock()
	defer s.mu.RUnlock()

	prefix := username + "\x00"
	result := make([]string, 0)

	for key := range s.directories {
		if !strings.HasPrefix(key, prefix) {
			continue
		}

		dir := strings.TrimPrefix(key, prefix)

		if path.Dir(dir) == dirPath {
			result = append(result, path.Base(dir))
		}
	}

	return result, nil
}

func (s *MemoryStore) DeleteDirectory(
	username, dirPath string,
) error {
	dirPath = path.Clean(dirPath)

	if dirPath == "/" {
		return ErrDirectoryNotEmpty
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	key := directoryKey(username, dirPath)
	if !s.directories[key] {
		return ErrDirectoryNotFound
	}

	prefix := dirPath + "/"

	for filePath, file := range s.files {
		if file.Owner == username &&
			strings.HasPrefix(filePath, prefix) {
			return ErrDirectoryNotEmpty
		}
	}

	for otherKey := range s.directories {
		if !strings.HasPrefix(otherKey, username+"\x00") ||
			otherKey == key {
			continue
		}

		otherPath := strings.TrimPrefix(
			otherKey,
			username+"\x00",
		)

		if strings.HasPrefix(otherPath, prefix) {
			return ErrDirectoryNotEmpty
		}
	}

	delete(s.directories, key)
	return nil
}
