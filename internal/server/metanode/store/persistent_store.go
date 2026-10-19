package store

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
)

var ErrNotLeader = errors.New("this MetaNode is not leader anymore: no writing")

var _ Store = (*PersistentStore)(nil)

// PersistentStore persists MemoryStore state as an atomic JSON snapshot.
type PersistentStore struct {
	*MemoryStore
	path     string
	mu       sync.Mutex
	canWrite func() bool
}

func NewPersistentStore(path string) (*PersistentStore, error) {
	if path == "" {
		return nil, errors.New("persistent state path cannot be empty")
	}

	p := &PersistentStore{
		MemoryStore: NewMemoryStore(),
		path:        path,
	}
	if err := p.Load(); err != nil {
		return nil, err
	}
	return p, nil
}

func (p *PersistentStore) SetCanWrite(fn func() bool) {
	p.canWrite = fn
}

// Sync writes the current state to disk. The MetaNode calls this after its role
// is established so state.json exists before the first user operation.
func (p *PersistentStore) Sync() error {
	return p.persist()
}

func (p *PersistentStore) Load() error {
	p.mu.Lock()
	defer p.mu.Unlock()

	data, err := os.ReadFile(p.path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	return p.MemoryStore.Restore(data)
}

func (p *PersistentStore) checkLeader() error {
	if p.canWrite != nil && !p.canWrite() {
		return ErrNotLeader
	}
	return nil
}

func (p *PersistentStore) persist() error {
	p.mu.Lock()
	defer p.mu.Unlock()

	if err := p.checkLeader(); err != nil {
		return err
	}
	data, err := p.MemoryStore.Snapshot()
	if err != nil {
		return err
	}

	dir := filepath.Dir(p.path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("failed to create persistent state directory for %s: %w", p.path, err)
	}
	tmp, err := os.CreateTemp(dir, ".state-*.tmp")
	if err != nil {
		return fmt.Errorf("failed to create temporary persistent state file for %s: %w", p.path, err)
	}
	tmpPath := tmp.Name()
	cleanup := func() {
		_ = tmp.Close()
		_ = os.Remove(tmpPath)
	}
	if err := tmp.Chmod(0o600); err != nil {
		cleanup()
		return fmt.Errorf("failed to set permissions on temporary persistent state file for %s: %w", p.path, err)
	}
	if _, err := tmp.Write(data); err != nil {
		cleanup()
		return fmt.Errorf("failed to write persistent state for %s: %w", p.path, err)
	}
	if err := tmp.Sync(); err != nil {
		cleanup()
		return fmt.Errorf("failed to sync persistent state for %s: %w", p.path, err)
	}
	if err := tmp.Close(); err != nil {
		_ = os.Remove(tmpPath)
		return fmt.Errorf("failed to close temporary persistent state file for %s: %w", p.path, err)
	}
	if err := replaceFile(tmpPath, p.path); err != nil {
		_ = os.Remove(tmpPath)
		return fmt.Errorf("failed to replace persistent state file at %s: %w", p.path, err)
	}
	return nil
}

func (p *PersistentStore) CreateFile(f FileMetadata) error {
	if err := p.checkLeader(); err != nil {
		return err
	}
	if err := p.MemoryStore.CreateFile(f); err != nil {
		return err
	}
	return p.persist()
}

func (p *PersistentStore) UpdateFile(f FileMetadata) error {
	if err := p.checkLeader(); err != nil {
		return err
	}
	if err := p.MemoryStore.UpdateFile(f); err != nil {
		return err
	}
	return p.persist()
}

func (p *PersistentStore) ReplaceFileWithStaged(owner, targetPath, stagingPath string) (FileMetadata, error) {
	if err := p.checkLeader(); err != nil {
		return FileMetadata{}, err
	}
	oldFile, err := p.MemoryStore.ReplaceFileWithStaged(owner, targetPath, stagingPath)
	if err != nil {
		return FileMetadata{}, err
	}
	if err := p.persist(); err != nil {
		return oldFile, err
	}
	return oldFile, nil
}

func (p *PersistentStore) DeleteFile(owner, filePath string) error {
	if err := p.checkLeader(); err != nil {
		return err
	}
	if err := p.MemoryStore.DeleteFile(owner, filePath); err != nil {
		return err
	}
	return p.persist()
}

func (p *PersistentStore) SaveBlockLocation(b BlockMetadata) error {
	if err := p.checkLeader(); err != nil {
		return err
	}
	if err := p.MemoryStore.SaveBlockLocation(b); err != nil {
		return err
	}
	return p.persist()
}

func (p *PersistentStore) DeleteBlockLocation(blockID string) error {
	if err := p.checkLeader(); err != nil {
		return err
	}
	if err := p.MemoryStore.DeleteBlockLocation(blockID); err != nil {
		return err
	}
	return p.persist()
}

func (p *PersistentStore) ReportBlock(blockID, dataNodeID, hash string) error {
	if err := p.checkLeader(); err != nil {
		return err
	}
	if err := p.MemoryStore.ReportBlock(blockID, dataNodeID, hash); err != nil {
		return err
	}
	return p.persist()
}

func (p *PersistentStore) RegisterDataNode(d DataNodeInfo) error {
	if err := p.checkLeader(); err != nil {
		return err
	}
	if err := p.MemoryStore.RegisterDataNode(d); err != nil {
		return err
	}
	return p.persist()
}

func (p *PersistentStore) UpdateHeartbeat(id string, freeBytes int64) error {
	if err := p.checkLeader(); err != nil {
		return err
	}
	if err := p.MemoryStore.UpdateHeartbeat(id, freeBytes); err != nil {
		return err
	}
	return p.persist()
}

func (p *PersistentStore) SetDataNodeAlive(id string, alive bool) error {
	if err := p.checkLeader(); err != nil {
		return err
	}
	if err := p.MemoryStore.SetDataNodeAlive(id, alive); err != nil {
		return err
	}
	return p.persist()
}

func (p *PersistentStore) CreateUser(u UserInfo) error {
	if err := p.checkLeader(); err != nil {
		return err
	}
	if err := p.MemoryStore.CreateUser(u); err != nil {
		return err
	}
	return p.persist()
}

func (p *PersistentStore) UpdateQuotaUsed(username string, delta int64) error {
	if err := p.checkLeader(); err != nil {
		return err
	}
	if err := p.MemoryStore.UpdateQuotaUsed(username, delta); err != nil {
		return err
	}
	return p.persist()
}

func (p *PersistentStore) ReserveQuota(username string, delta, maxBytes int64) error {
	if err := p.checkLeader(); err != nil {
		return err
	}
	if err := p.MemoryStore.ReserveQuota(username, delta, maxBytes); err != nil {
		return err
	}
	return p.persist()
}

func (p *PersistentStore) CreateDirectory(username, dirPath string) error {
	if err := p.checkLeader(); err != nil {
		return err
	}
	if err := p.MemoryStore.CreateDirectory(username, dirPath); err != nil {
		return err
	}
	return p.persist()
}

func (p *PersistentStore) DeleteDirectory(username, dirPath string) error {
	if err := p.checkLeader(); err != nil {
		return err
	}
	if err := p.MemoryStore.DeleteDirectory(username, dirPath); err != nil {
		return err
	}
	return p.persist()
}

func (p *PersistentStore) EnqueueBlockDeletion(dataNodeID, blockID string) error {
	if err := p.checkLeader(); err != nil {
		return err
	}
	if err := p.MemoryStore.EnqueueBlockDeletion(dataNodeID, blockID); err != nil {
		return err
	}
	return p.persist()
}

// DequeueBlockDeletions is kept for callers that explicitly need destructive
// dequeue semantics. The heartbeat protocol uses List/Ack for reliable retry.
func (p *PersistentStore) DequeueBlockDeletions(dataNodeID string) ([]string, error) {
	if err := p.checkLeader(); err != nil {
		return nil, err
	}
	ids, err := p.MemoryStore.DequeueBlockDeletions(dataNodeID)
	if err != nil {
		return nil, err
	}
	if err := p.persist(); err != nil {
		return ids, err
	}
	return ids, nil
}

func (p *PersistentStore) ListBlockDeletions(dataNodeID string) ([]string, error) {
	return p.MemoryStore.ListBlockDeletions(dataNodeID)
}

func (p *PersistentStore) AckBlockDeletions(dataNodeID string, blockIDs []string) error {
	if len(blockIDs) == 0 {
		return nil
	}
	if err := p.checkLeader(); err != nil {
		return err
	}
	if err := p.MemoryStore.AckBlockDeletions(dataNodeID, blockIDs); err != nil {
		return err
	}
	return p.persist()
}

func (p *PersistentStore) IsBlockReferenced(blockID string) bool {
	return p.MemoryStore.IsBlockReferenced(blockID)
}
