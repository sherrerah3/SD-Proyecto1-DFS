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

// --- Files ---

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
			"file not found: %s",
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
		return fmt.Errorf("file not found: %s", f.Path)
	}

	s.files[key] = f
	return nil
}

// ReplaceFileWithStaged publishes temporary metadata to the final path
// under the same mutex. The previous file is returned to clean up its blocks.
func (s *MemoryStore) ReplaceFileWithStaged(
	owner, targetPath, stagingPath string,
) (FileMetadata, error) {
	if targetPath == stagingPath {
		return FileMetadata{}, errors.New(
			"temporary path and destination path cannot be identical",
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
			"destination file not found: %s",
			targetPath,
		)
	}

	if !stagingExists {
		return FileMetadata{}, fmt.Errorf(
			"temporary file not found: %s",
			stagingPath,
		)
	}

	if stagedFile.SHA256 == "" {
		return FileMetadata{}, errors.New(
			"the temporary version is not complete yet",
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
		return fmt.Errorf("file not found: %s", filePath)
	}

	delete(s.files, key)
	return nil
}

// ListFiles returns the user's files under dirPath.
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

// --- Blocks ---

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
			"block not found: %s",
			blockID,
		)
	}

	return b, nil
}

// ListBlockLocations returns a snapshot of all known block locations.
func (s *MemoryStore) ListBlockLocations() ([]BlockMetadata, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	out := make([]BlockMetadata, 0, len(s.blocks))
	for _, block := range s.blocks {
		block.DataNodes = append([]string(nil), block.DataNodes...)
		if block.Confirmations != nil {
			copyConfirmations := make(map[string]string, len(block.Confirmations))
			for id, hash := range block.Confirmations {
				copyConfirmations[id] = hash
			}
			block.Confirmations = copyConfirmations
		}
		out = append(out, block)
	}
	return out, nil
}

func (s *MemoryStore) ReportBlock(
	blockID, dataNodeID, hash string,
) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	b, ok := s.blocks[blockID]
	if !ok {
		return fmt.Errorf("block not found: %s", blockID)
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

	for _, queued := range s.pendingDel[dataNodeID] {
		if queued == blockID {
			return nil
		}
	}
	s.pendingDel[dataNodeID] = append(s.pendingDel[dataNodeID], blockID)
	return nil
}

// ListBlockDeletions returns pending deletions without consuming them. The
// DataNode acknowledges a deletion only after a later inventory proves that
// the block is no longer present on disk.
func (s *MemoryStore) ListBlockDeletions(dataNodeID string) ([]string, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	ids := s.pendingDel[dataNodeID]
	return append([]string(nil), ids...), nil
}

// AckBlockDeletions removes only the specified items from the durable queue.
func (s *MemoryStore) AckBlockDeletions(dataNodeID string, blockIDs []string) error {
	if len(blockIDs) == 0 {
		return nil
	}

	acked := make(map[string]struct{}, len(blockIDs))
	for _, id := range blockIDs {
		acked[id] = struct{}{}
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	current := s.pendingDel[dataNodeID]
	kept := current[:0]
	for _, id := range current {
		if _, ok := acked[id]; !ok {
			kept = append(kept, id)
		}
	}
	if len(kept) == 0 {
		delete(s.pendingDel, dataNodeID)
	} else {
		s.pendingDel[dataNodeID] = append([]string(nil), kept...)
	}
	return nil
}

// IsBlockReferenced reports whether a current file entry still uses blockID.
// It intentionally includes staged uploads, because their blocks are valid
// until the upload is committed or aborted.
func (s *MemoryStore) IsBlockReferenced(blockID string) bool {
	s.mu.RLock()
	defer s.mu.RUnlock()

	for _, file := range s.files {
		for _, id := range file.BlockIDs {
			if id == blockID {
				return true
			}
		}
	}
	return false
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

// --- Users ---

func (s *MemoryStore) CreateUser(u UserInfo) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if _, exists := s.users[u.Username]; exists {
		return fmt.Errorf("user %s already exists", u.Username)
	}

	s.users[u.Username] = u
	return nil
}

func (s *MemoryStore) GetUser(username string) (UserInfo, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	u, ok := s.users[username]
	if !ok {
		return UserInfo{}, fmt.Errorf("user not found: %s", username)
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
		return fmt.Errorf("user not found: %s", username)
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
		return fmt.Errorf("user not found: %s", username)
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

// --- Snapshot for persistence ---

type snapshot struct {
	Files       map[string]FileMetadata  `json:"files"`
	Blocks      map[string]BlockMetadata `json:"blocks"`
	DataNodes   map[string]DataNodeInfo  `json:"datanodes"`
	Users       map[string]UserInfo      `json:"users"`
	Directories map[string]bool          `json:"directories"`
	PendingDel  map[string][]string      `json:"pending_deletions"`
}

func (s *MemoryStore) Snapshot() ([]byte, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	return json.Marshal(snapshot{
		Files:       s.files,
		Blocks:      s.blocks,
		DataNodes:   s.datanodes,
		Users:       s.users,
		Directories: s.directories,
		PendingDel:  s.pendingDel,
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
	s.datanodes = orEmpty(snap.DataNodes)
	s.users = orEmpty(snap.Users)
	s.directories = orEmpty(snap.Directories)
	s.pendingDel = orEmpty(snap.PendingDel)

	return nil
}

func orEmpty[K comparable, V any](m map[K]V) map[K]V {
	if m == nil {
		return make(map[K]V)
	}

	return m
}

var (
	ErrDirectoryNotFound = errors.New("directory not found")
	ErrDirectoryExists   = errors.New("directory already exists")
	ErrDirectoryNotEmpty = errors.New("directory is not empty")
	ErrQuotaExceeded     = errors.New("storage quota exceeded")
	ErrFileAlreadyExists = errors.New("a file already exists at this path")
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
