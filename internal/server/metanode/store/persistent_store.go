package store

import (
	"errors"
	"os"
	"sync"
)

var ErrNotLeader = errors.New("este metanodo ya no es el lider: escritura rechazada")

// Valida en tiempo de compilación: *PersistentStore debe cumplir Store.
var _ Store = (*PersistentStore)(nil)

// PersistentStore envuelve un MemoryStore y guarda un snapshot en disco tras cada cambio.
type PersistentStore struct {
	*MemoryStore             // hereda TODOS los metodos de MemoryStore
	path         string      // ej. /mnt/dfsha-shared/state.json
	mu           sync.Mutex  // un guardado a la vez
	canWrite     func() bool // lo conecta failover.go: "sigo teniendo el candado?"
}

func NewPersistentStore(path string) (*PersistentStore, error) {
	p := &PersistentStore{MemoryStore: NewMemoryStore(), path: path}
	if err := p.Load(); err != nil {
		return nil, err
	}
	return p, nil
}

// SetCanWrite conecta el chequeo de liderazgo que decide si se permite escribir.
func (p *PersistentStore) SetCanWrite(fn func() bool) {
	p.canWrite = fn
}

// Load lee state.json (si existe). Lo llama el respaldo al tomar el control.
func (p *PersistentStore) Load() error {
	data, err := os.ReadFile(p.path)
	if errors.Is(err, os.ErrNotExist) {
		return nil // primera vez: no hay nada que cargar
	}
	if err != nil {
		return err
	}
	return p.MemoryStore.Restore(data)
}

func (p *PersistentStore) persist() error {
	p.mu.Lock()
	defer p.mu.Unlock()

	// Si ya perdimos el candado, NO escribir: otro metanodo es el lider ahora.
	if p.canWrite != nil && !p.canWrite() {
		return ErrNotLeader
	}

	data, err := p.MemoryStore.Snapshot()
	if err != nil {
		return err
	}

	// Escritura atomica: a un temporal y luego rename, para no dejar el archivo a medias.
	tmp := p.path + ".tmp"
	f, err := os.OpenFile(tmp, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o600)
	if err != nil {
		return err
	}
	if _, err := f.Write(data); err != nil {
		f.Close()
		return err
	}
	if err := f.Sync(); err != nil { // forzar que llegue al disco
		f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	return os.Rename(tmp, p.path)
}

// Solo se sobreescriben los metodos que MODIFICAN archivos, bloques o usuarios.

func (p *PersistentStore) CreateFile(f FileMetadata) error {
	if err := p.MemoryStore.CreateFile(f); err != nil {
		return err
	}
	return p.persist()
}

func (p *PersistentStore) UpdateFile(f FileMetadata) error {
	if err := p.MemoryStore.UpdateFile(f); err != nil {
		return err
	}
	return p.persist()
}

func (p *PersistentStore) DeleteFile(path string) error {
	if err := p.MemoryStore.DeleteFile(path); err != nil {
		return err
	}
	return p.persist()
}

func (p *PersistentStore) SaveBlockLocation(b BlockMetadata) error {
	if err := p.MemoryStore.SaveBlockLocation(b); err != nil {
		return err
	}
	return p.persist()
}

func (p *PersistentStore) CreateUser(u UserInfo) error {
	if err := p.MemoryStore.CreateUser(u); err != nil {
		return err
	}
	return p.persist()
}

func (p *PersistentStore) UpdateQuotaUsed(username string, delta int64) error {
	if err := p.MemoryStore.UpdateQuotaUsed(username, delta); err != nil {
		return err
	}
	return p.persist()
}

func (p *PersistentStore) CreateDirectory(username, dirPath string) error {
	if err := p.MemoryStore.CreateDirectory(username, dirPath); err != nil {
		return err
	}
	return p.persist()
}
