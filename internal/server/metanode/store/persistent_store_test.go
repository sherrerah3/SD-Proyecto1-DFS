package store

import (
	"fmt"
	"path/filepath"
	"testing"
)

func TestPersistentStorePersistsAcrossInstances(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")

	// Primera instancia escribe datos.
	p1, err := NewPersistentStore(path)
	if err != nil {
		t.Fatalf("NewPersistentStore failed: %v", err)
	}
	if err := p1.CreateUser(UserInfo{Username: "ana", PasswordHash: "hash"}); err != nil {
		t.Fatalf("CreateUser failed: %v", err)
	}
	if err := p1.CreateFile(FileMetadata{Path: "/a.txt", Owner: "ana", SizeBytes: 100}); err != nil {
		t.Fatalf("CreateFile failed: %v", err)
	}

	// Segunda instancia carga el mismo state.json y debe ver todo.
	p2, err := NewPersistentStore(path)
	if err != nil {
		t.Fatalf("NewPersistentStore (second instance) failed: %v", err)
	}
	if _, err := p2.GetUser("ana"); err != nil {
		t.Fatalf("user was not persisted: %v", err)
	}
	f, err := p2.GetFile("ana", "/a.txt")
	if err != nil {
		t.Fatalf("file was not persisted: %v", err)
	}
	if f.SizeBytes != 100 {
		t.Fatalf("SizeBytes = %d, expected 100", f.SizeBytes)
	}
}

func TestPersistentStorePersistsDataNodesAndDeletionQueue(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")

	p1, err := NewPersistentStore(path)
	if err != nil {
		t.Fatalf("NewPersistentStore failed: %v", err)
	}
	if err := p1.RegisterDataNode(DataNodeInfo{ID: "dn-1", Address: "127.0.0.1:9000"}); err != nil {
		t.Fatalf("RegisterDataNode failed: %v", err)
	}
	if err := p1.EnqueueBlockDeletion("dn-1", "obsolete-block"); err != nil {
		t.Fatalf("EnqueueBlockDeletion failed: %v", err)
	}
	if err := p1.SetDataNodeAlive("dn-1", false); err != nil {
		t.Fatalf("SetDataNodeAlive failed: %v", err)
	}

	p2, err := NewPersistentStore(path)
	if err != nil {
		t.Fatalf("NewPersistentStore (second instance) failed: %v", err)
	}
	nodes, err := p2.ListDataNodes()
	if err != nil {
		t.Fatalf("ListDataNodes failed: %v", err)
	}
	if len(nodes) != 1 {
		t.Fatalf("expected one persisted DataNode, got %d", len(nodes))
	}
	if nodes[0].Alive {
		t.Fatalf("expected DataNode alive status to persist as false")
	}
	pending, err := p2.ListBlockDeletions("dn-1")
	if err != nil {
		t.Fatalf("ListBlockDeletions failed: %v", err)
	}
	if len(pending) != 1 || pending[0] != "obsolete-block" {
		t.Fatalf("expected persisted pending deletion, got %v", pending)
	}
}

func TestPersistentStoreRejectsWritesWhenNotLeader(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	p, err := NewPersistentStore(path)
	if err != nil {
		t.Fatalf("NewPersistentStore failed: %v", err)
	}

	// Simula haber perdido el liderazgo.
	p.SetCanWrite(func() bool { return false })

	if err := p.CreateUser(UserInfo{Username: "ana"}); err != ErrNotLeader {
		t.Fatalf("error = %v, expected ErrNotLeader", err)
	}
}

func TestPersistentStoreAllowsWritesWhenLeader(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	p, err := NewPersistentStore(path)
	if err != nil {
		t.Fatalf("NewPersistentStore failed: %v", err)
	}

	p.SetCanWrite(func() bool { return true })

	if err := p.CreateUser(UserInfo{Username: "ana"}); err != nil {
		t.Fatalf("CreateUser should not fail while this node is leader: %v", err)
	}
}

func TestPersistentStoreStartsWithoutExistingFile(t *testing.T) {
	// Si state.json no existe, arranca vacío sin error.
	path := filepath.Join(t.TempDir(), "no-existe.json")
	p, err := NewPersistentStore(path)
	if err != nil {
		t.Fatalf("NewPersistentStore should start with empty state: %v", err)
	}
	if _, err := p.GetUser("ana"); err == nil {
		t.Fatalf("a newly created store should not contain users")
	}
}

func TestPersistentStorePreservesUsersAfterRepeatedStateWrites(t *testing.T) {
	statePath := filepath.Join(t.TempDir(), "metanode", "state.json")

	first, err := NewPersistentStore(statePath)
	if err != nil {
		t.Fatalf("NewPersistentStore failed: %v", err)
	}
	if err := first.Sync(); err != nil {
		t.Fatalf("initial Sync failed: %v", err)
	}
	if err := first.CreateUser(UserInfo{
		Username:       "seb",
		PasswordHash:   "persisted-password-hash",
		QuotaUsedBytes: 42,
	}); err != nil {
		t.Fatalf("CreateUser failed: %v", err)
	}

	// Force several replacements of an already existing state.json file.
	for i := 0; i < 3; i++ {
		id := fmt.Sprintf("datanode-%d", i+1)
		if err := first.RegisterDataNode(DataNodeInfo{ID: id, Address: fmt.Sprintf("localhost:%d", 9000+i)}); err != nil {
			t.Fatalf("RegisterDataNode(%s) failed: %v", id, err)
		}
	}
	if err := first.CreateFile(FileMetadata{
		Name:      "hello.txt",
		Path:      "/hello.txt",
		Owner:     "seb",
		SizeBytes: 100,
		SHA256:    "checksum",
	}); err != nil {
		t.Fatalf("CreateFile failed: %v", err)
	}

	second, err := NewPersistentStore(statePath)
	if err != nil {
		t.Fatalf("reopening persistent state failed: %v", err)
	}
	user, err := second.GetUser("seb")
	if err != nil {
		t.Fatalf("user was not restored after repeated writes: %v", err)
	}
	if user.PasswordHash != "persisted-password-hash" {
		t.Fatalf("restored password hash = %q, expected original hash", user.PasswordHash)
	}
	if user.QuotaUsedBytes != 42 {
		t.Fatalf("restored quota = %d, expected 42", user.QuotaUsedBytes)
	}
	if _, err := second.GetFile("seb", "/hello.txt"); err != nil {
		t.Fatalf("file metadata was not restored: %v", err)
	}
}
