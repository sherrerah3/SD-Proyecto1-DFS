package store

import (
	"errors"
	"os"
	"sync"
)

var ErrNotLeader = errors.New(
	"este metanodo ya no es el lider: escritura rechazada",
)

var _ Store = (*PersistentStore)(nil)

// PersistentStore guarda el estado del MemoryStore en disco.
type PersistentStore struct {
	*MemoryStore
	path     string
	mu       sync.Mutex
	canWrite func() bool
}

func NewPersistentStore(path string) (*PersistentStore, error) {
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

func (p *PersistentStore) Load() error {
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

	if p.canWrite != nil && !p.canWrite() {
		return ErrNotLeader
	}

	data, err := p.MemoryStore.Snapshot()
	if err != nil {
		return err
	}

	tmp := p.path + ".tmp"

	f, err := os.OpenFile(
		tmp,
		os.O_WRONLY|os.O_CREATE|os.O_TRUNC,
		0o600,
	)
	if err != nil {
		return err
	}

	if _, err := f.Write(data); err != nil {
		_ = f.Close()
		return err
	}

	if err := f.Sync(); err != nil {
		_ = f.Close()
		return err
	}

	if err := f.Close(); err != nil {
		return err
	}

	return os.Rename(tmp, p.path)
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

// ReplaceFileWithStaged persiste el commit de la nueva versión.
func (p *PersistentStore) ReplaceFileWithStaged(
	owner, targetPath, stagingPath string,
) (FileMetadata, error) {
	if err := p.checkLeader(); err != nil {
		return FileMetadata{}, err
	}

	oldFile, err := p.MemoryStore.ReplaceFileWithStaged(
		owner,
		targetPath,
		stagingPath,
	)
	if err != nil {
		return FileMetadata{}, err
	}

	if err := p.persist(); err != nil {
		return oldFile, err
	}

	return oldFile, nil
}

func (p *PersistentStore) DeleteFile(
	owner, filePath string,
) error {
	if err := p.checkLeader(); err != nil {
		return err
	}

	if err := p.MemoryStore.DeleteFile(owner, filePath); err != nil {
		return err
	}

	return p.persist()
}

func (p *PersistentStore) SaveBlockLocation(
	b BlockMetadata,
) error {
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

func (p *PersistentStore) ReportBlock(
	blockID, dataNodeID, hash string,
) error {
	if err := p.checkLeader(); err != nil {
		return err
	}

	if err := p.MemoryStore.ReportBlock(
		blockID,
		dataNodeID,
		hash,
	); err != nil {
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

func (p *PersistentStore) UpdateQuotaUsed(
	username string,
	delta int64,
) error {
	if err := p.checkLeader(); err != nil {
		return err
	}

	if err := p.MemoryStore.UpdateQuotaUsed(username, delta); err != nil {
		return err
	}

	return p.persist()
}

func (p *PersistentStore) ReserveQuota(
	username string,
	delta, maxBytes int64,
) error {
	if err := p.checkLeader(); err != nil {
		return err
	}

	if err := p.MemoryStore.ReserveQuota(
		username,
		delta,
		maxBytes,
	); err != nil {
		return err
	}

	return p.persist()
}

func (p *PersistentStore) CreateDirectory(
	username, dirPath string,
) error {
	if err := p.checkLeader(); err != nil {
		return err
	}

	if err := p.MemoryStore.CreateDirectory(
		username,
		dirPath,
	); err != nil {
		return err
	}

	return p.persist()
}

func (p *PersistentStore) DeleteDirectory(
	username, dirPath string,
) error {
	if err := p.checkLeader(); err != nil {
		return err
	}

	if err := p.MemoryStore.DeleteDirectory(
		username,
		dirPath,
	); err != nil {
		return err
	}

	return p.persist()
}
