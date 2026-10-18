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

// Valida en tiempo de compilación: *MemoryStore debe cumplir Store.
var _ Store = (*MemoryStore)(nil)

// MemoryStore implementa Store en memoria, protegido por un Mutex.
type MemoryStore struct {
	mu          sync.RWMutex
	files       map[string]FileMetadata  // key: owner\x00path
	blocks      map[string]BlockMetadata // key: blockID
	datanodes   map[string]DataNodeInfo  // key: datanode ID
	users       map[string]UserInfo      // key: username
	directories map[string]bool          // key: owner\x00dir
	pendingDel  map[string][]string      // key: datanode ID -> blockIDs por borrar
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

// fileKey separa el espacio de rutas de cada usuario
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

func (s *MemoryStore) GetFile(owner, filePath string) (FileMetadata, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	f, ok := s.files[fileKey(owner, filePath)]
	if !ok {
		return FileMetadata{}, fmt.Errorf("archivo no encontrado: %s", filePath)
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

func (s *MemoryStore) DeleteFile(owner, filePath string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	key := fileKey(owner, filePath)
	if _, ok := s.files[key]; !ok {
		return fmt.Errorf("archivo no encontrado: %s", filePath)
	}

	delete(s.files, key)

	return nil
}

// ListFiles devuelve los archivos del usuario bajo dirPath, sin orden
func (s *MemoryStore) ListFiles(owner, dirPath string) ([]FileMetadata, error) {
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

func (s *MemoryStore) GetBlockLocation(blockID string) (BlockMetadata, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	b, ok := s.blocks[blockID]
	if !ok {
		return BlockMetadata{}, fmt.Errorf("bloque no encontrado: %s", blockID)
	}

	return b, nil
}

// ReportBlock anota la confirmación de un DataNode
func (s *MemoryStore) ReportBlock(blockID, dataNodeID, hash string) error {
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

// DeleteBlockLocation olvida la ubicación de un bloque
func (s *MemoryStore) DeleteBlockLocation(blockID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	delete(s.blocks, blockID)
	return nil
}

// EnqueueBlockDeletion encola un borrado para el próximo heartbeat del DataNode
func (s *MemoryStore) EnqueueBlockDeletion(dataNodeID, blockID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.pendingDel[dataNodeID] = append(s.pendingDel[dataNodeID], blockID)
	return nil
}

// DequeueBlockDeletions devuelve y limpia los borrados pendientes
func (s *MemoryStore) DequeueBlockDeletions(dataNodeID string) ([]string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	ids := s.pendingDel[dataNodeID]
	delete(s.pendingDel, dataNodeID)
	return ids, nil
}

// --- DataNodes ---

// RegisterDataNode sobrescribe si el id ya existe: un nodo reiniciado se re-registra.
func (s *MemoryStore) RegisterDataNode(d DataNodeInfo) error {
	s.mu.Lock()
	defer s.mu.Unlock()

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
		return ErrDataNodeNotFound
	}

	// Un heartbeat revive un nodo que estaba marcado como muerto.
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

// ListDataNodes devuelve todos los datanodes, vivos y muertos.
func (s *MemoryStore) ListDataNodes() ([]DataNodeInfo, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	out := make([]DataNodeInfo, 0, len(s.datanodes))
	for _, d := range s.datanodes {
		out = append(out, d)
	}

	return out, nil
}

// SetDataNodeAlive marca un datanode como vivo o muerto.
func (s *MemoryStore) SetDataNodeAlive(id string, alive bool) error {
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

// ReserveQuota suma a la cuota solo si no supera maxBytes (atómico)
func (s *MemoryStore) ReserveQuota(username string, deltaBytes, maxBytes int64) error {
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

// CountUsers devuelve cuántos usuarios hay
func (s *MemoryStore) CountUsers() int {
	s.mu.RLock()
	defer s.mu.RUnlock()

	return len(s.users)
}

// --- Snapshot para persistencia ---

// snapshot es el estado persistente, sin datanodes (su estado se reconstruye solo).
type snapshot struct {
	Files       map[string]FileMetadata  `json:"files"`
	Blocks      map[string]BlockMetadata `json:"blocks"`
	Users       map[string]UserInfo      `json:"users"`
	Directories map[string]bool          `json:"directories"`
}

// Snapshot serializa el estado persistente (sin DataNodes).
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

// Restore reemplaza el estado con el contenido de un snapshot.
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

// orEmpty devuelve un mapa vacío si el recibido es nil.
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

func (s *MemoryStore) DirectoryExists(username, dirPath string) bool {
	dirPath = path.Clean(dirPath)

	if dirPath == "/" {
		return true
	}

	s.mu.RLock()
	defer s.mu.RUnlock()

	return s.directories[directoryKey(username, dirPath)]
}

func (s *MemoryStore) CreateDirectory(username, dirPath string) error {
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
	if parent != "/" && !s.directories[directoryKey(username, parent)] {
		return ErrDirectoryNotFound
	}

	s.directories[key] = true
	return nil
}

// ListDirectories devuelve los directorios hijos directos de dirPath
// que pertenecen al usuario indicado.
func (s *MemoryStore) ListDirectories(
	username string,
	dirPath string,
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

		// La clave tiene el formato username + "\x00" + ruta.
		dir := strings.TrimPrefix(key, prefix)

		// Solo incluir los hijos directos, no todos los descendientes.
		if path.Dir(dir) == dirPath {
			result = append(result, path.Base(dir))
		}
	}

	return result, nil
}

func (s *MemoryStore) DeleteDirectory(username, dirPath string) error {
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

	// A directory cannot be removed while it contains files.
	prefix := dirPath + "/"
	for filePath, file := range s.files {
		if file.Owner == username && strings.HasPrefix(filePath, prefix) {
			return ErrDirectoryNotEmpty
		}
	}

	// A directory cannot be removed while it contains subdirectories.
	for otherKey := range s.directories {
		if !strings.HasPrefix(otherKey, username+"\x00") || otherKey == key {
			continue
		}

		otherPath := strings.TrimPrefix(otherKey, username+"\x00")
		if strings.HasPrefix(otherPath, prefix) {
			return ErrDirectoryNotEmpty
		}
	}

	delete(s.directories, key)
	return nil
}
