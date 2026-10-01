package store

import (
	"fmt"
	"strings"
	"sync"
	"time"
)

// Aserción en tiempo de compilación: *MemoryStore debe cumplir Store.
var _ Store = (*MemoryStore)(nil)

// MemoryStore implementa Store en memoria, protegido por un único sync.RWMutex.
type MemoryStore struct {
	mu        sync.RWMutex
	files     map[string]FileMetadata  // key: path
	blocks    map[string]BlockMetadata // key: blockID
	datanodes map[string]DataNodeInfo  // key: datanode ID
	users     map[string]UserInfo      // key: username
}

func NewMemoryStore() *MemoryStore {
	return &MemoryStore{
		files:     make(map[string]FileMetadata),
		blocks:    make(map[string]BlockMetadata),
		datanodes: make(map[string]DataNodeInfo),
		users:     make(map[string]UserInfo),
	}
}

// --- Archivos ---

func (s *MemoryStore) CreateFile(f FileMetadata) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if _, exists := s.files[f.Path]; exists {
		return fmt.Errorf("el archivo %s ya existe", f.Path)
	}

	s.files[f.Path] = f

	return nil
}

func (s *MemoryStore) GetFile(path string) (FileMetadata, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	f, ok := s.files[path]
	if !ok {
		return FileMetadata{}, fmt.Errorf("archivo no encontrado: %s", path)
	}

	return f, nil
}

func (s *MemoryStore) DeleteFile(path string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if _, ok := s.files[path]; !ok {
		return fmt.Errorf("archivo no encontrado: %s", path)
	}

	delete(s.files, path)

	return nil
}

// ListFiles devuelve los archivos bajo dirPath, sin orden garantizado.
func (s *MemoryStore) ListFiles(dirPath string) ([]FileMetadata, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	prefix := dirPath
	if !strings.HasSuffix(prefix, "/") {
		prefix += "/"
	}

	result := make([]FileMetadata, 0)

	for path, f := range s.files {
		if strings.HasPrefix(path, prefix) {
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

func (s *MemoryStore) GetBlockLocation(blockID string) (BlockMetadata, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	b, ok := s.blocks[blockID]
	if !ok {
		return BlockMetadata{}, fmt.Errorf("bloque no encontrado: %s", blockID)
	}

	return b, nil
}

// --- DataNodes ---

func (s *MemoryStore) RegisterDataNode(d DataNodeInfo) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if _, exists := s.datanodes[d.ID]; exists {
		return fmt.Errorf("el datanode %s ya está registrado", d.ID)
	}

	d.Alive = true
	d.LastHeartbeat = time.Now()
	s.datanodes[d.ID] = d

	return nil
}

func (s *MemoryStore) UpdateHeartbeat(id string, freeBytes int64) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	d, ok := s.datanodes[id]
	if !ok {
		return fmt.Errorf("datanode no encontrado: %s", id)
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

func (s *MemoryStore) UpdateQuotaUsed(username string, deltaBytes int64) error {
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
