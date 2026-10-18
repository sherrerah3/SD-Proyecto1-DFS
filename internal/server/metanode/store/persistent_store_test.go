package store

import (
	"path/filepath"
	"testing"
)

func TestPersistentStorePersisteEntreInstancias(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")

	// Primera instancia escribe datos.
	p1, err := NewPersistentStore(path)
	if err != nil {
		t.Fatalf("NewPersistentStore falló: %v", err)
	}
	if err := p1.CreateUser(UserInfo{Username: "ana", PasswordHash: "hash"}); err != nil {
		t.Fatalf("CreateUser falló: %v", err)
	}
	if err := p1.CreateFile(FileMetadata{Path: "/a.txt", Owner: "ana", SizeBytes: 100}); err != nil {
		t.Fatalf("CreateFile falló: %v", err)
	}

	// Segunda instancia carga el mismo state.json y debe ver todo.
	p2, err := NewPersistentStore(path)
	if err != nil {
		t.Fatalf("NewPersistentStore (segunda) falló: %v", err)
	}
	if _, err := p2.GetUser("ana"); err != nil {
		t.Fatalf("el usuario no persistió: %v", err)
	}
	f, err := p2.GetFile("ana", "/a.txt")
	if err != nil {
		t.Fatalf("el archivo no persistió: %v", err)
	}
	if f.SizeBytes != 100 {
		t.Fatalf("SizeBytes = %d, se esperaba 100", f.SizeBytes)
	}
}

func TestPersistentStoreNoPersisteDataNodes(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")

	p1, err := NewPersistentStore(path)
	if err != nil {
		t.Fatalf("NewPersistentStore falló: %v", err)
	}
	if err := p1.RegisterDataNode(DataNodeInfo{ID: "dn-1", Address: "127.0.0.1:9000"}); err != nil {
		t.Fatalf("RegisterDataNode falló: %v", err)
	}
	// Un cambio persistente dispara el guardado del snapshot.
	if err := p1.CreateUser(UserInfo{Username: "ana"}); err != nil {
		t.Fatalf("CreateUser falló: %v", err)
	}

	// La segunda instancia no debe ver datanodes: el snapshot no los incluye.
	p2, err := NewPersistentStore(path)
	if err != nil {
		t.Fatalf("NewPersistentStore (segunda) falló: %v", err)
	}
	nodes, _ := p2.ListDataNodes()
	if len(nodes) != 0 {
		t.Fatalf("los datanodes no debían persistir, hay %d", len(nodes))
	}
}

func TestPersistentStoreRechazaSiNoEsLider(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	p, err := NewPersistentStore(path)
	if err != nil {
		t.Fatalf("NewPersistentStore falló: %v", err)
	}

	// Simula haber perdido el liderazgo.
	p.SetCanWrite(func() bool { return false })

	if err := p.CreateUser(UserInfo{Username: "ana"}); err != ErrNotLeader {
		t.Fatalf("error = %v, se esperaba ErrNotLeader", err)
	}
}

func TestPersistentStorePermiteSiEsLider(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	p, err := NewPersistentStore(path)
	if err != nil {
		t.Fatalf("NewPersistentStore falló: %v", err)
	}

	p.SetCanWrite(func() bool { return true })

	if err := p.CreateUser(UserInfo{Username: "ana"}); err != nil {
		t.Fatalf("siendo líder, CreateUser no debía fallar: %v", err)
	}
}

func TestPersistentStoreArranqueSinArchivo(t *testing.T) {
	// Si state.json no existe, arranca vacío sin error.
	path := filepath.Join(t.TempDir(), "no-existe.json")
	p, err := NewPersistentStore(path)
	if err != nil {
		t.Fatalf("NewPersistentStore debió arrancar vacío: %v", err)
	}
	if _, err := p.GetUser("ana"); err == nil {
		t.Fatalf("no debía haber usuarios en un store recién creado")
	}
}

func TestPersistentStoreModifyFilePersisteYRespetaLiderazgo(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	p, err := NewPersistentStore(path)
	if err != nil {
		t.Fatalf("NewPersistentStore falló: %v", err)
	}
	if err := p.CreateFile(FileMetadata{Path: "/a.txt", Owner: "ana", SizeBytes: 1}); err != nil {
		t.Fatalf("CreateFile falló: %v", err)
	}

	pending := &PendingVersion{SizeBytes: 5, BlockIDs: []string{"b1"}}
	if _, err := p.ModifyFile("ana", "/a.txt", func(f *FileMetadata) error {
		f.Pending = pending
		return nil
	}); err != nil {
		t.Fatalf("ModifyFile falló: %v", err)
	}

	// La versión pendiente debe sobrevivir a un reinicio.
	p2, err := NewPersistentStore(path)
	if err != nil {
		t.Fatalf("NewPersistentStore (segunda) falló: %v", err)
	}
	f, _ := p2.GetFile("ana", "/a.txt")
	if f.Pending == nil || f.Pending.SizeBytes != 5 {
		t.Fatalf("la versión pendiente no persistió: %+v", f.Pending)
	}

	// Sin liderazgo no debe modificar ni la memoria.
	p.SetCanWrite(func() bool { return false })
	if _, err := p.ModifyFile("ana", "/a.txt", func(f *FileMetadata) error {
		f.SizeBytes = 99
		return nil
	}); err != ErrNotLeader {
		t.Fatalf("error = %v, se esperaba ErrNotLeader", err)
	}
	got, _ := p.GetFile("ana", "/a.txt")
	if got.SizeBytes != 1 {
		t.Fatalf("la memoria no debía cambiar sin liderazgo, SizeBytes = %d", got.SizeBytes)
	}
}
