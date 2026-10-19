package store

import "testing"

func TestCreateFile(t *testing.T) {
	existing := FileMetadata{Name: "a.txt", Path: "/a.txt", Owner: "ana"}

	tests := []struct {
		name    string
		input   FileMetadata
		wantErr bool
	}{
		{
			name:    "creates a new file",
			input:   FileMetadata{Name: "b.txt", Path: "/b.txt", Owner: "ana"},
			wantErr: false,
		},
		{
			name:    "rechaza un path duplicado",
			input:   existing,
			wantErr: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			s := NewMemoryStore()
			if err := s.CreateFile(existing); err != nil {
				t.Fatalf("setup failed: %v", err)
			}

			err := s.CreateFile(tc.input)
			if (err != nil) != tc.wantErr {
				t.Fatalf("CreateFile() error = %v, wantErr = %v", err, tc.wantErr)
			}
		})
	}
}

func TestGetFile(t *testing.T) {
	stored := FileMetadata{Name: "a.txt", Path: "/a.txt", Owner: "ana", SizeBytes: 10}

	tests := []struct {
		name    string
		path    string
		wantErr bool
	}{
		{name: "gets an existing file", path: "/a.txt", wantErr: false},
		{name: "error when missing", path: "/no.txt", wantErr: true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			s := NewMemoryStore()
			if err := s.CreateFile(stored); err != nil {
				t.Fatalf("setup failed: %v", err)
			}

			got, err := s.GetFile("ana", tc.path)
			if (err != nil) != tc.wantErr {
				t.Fatalf("GetFile() error = %v, wantErr = %v", err, tc.wantErr)
			}

			if !tc.wantErr && got.Path != stored.Path {
				t.Fatalf("GetFile() returned path %q, expected %q", got.Path, stored.Path)
			}
		})
	}
}

func TestDeleteFile(t *testing.T) {
	stored := FileMetadata{Name: "a.txt", Path: "/a.txt", Owner: "ana"}

	tests := []struct {
		name    string
		path    string
		wantErr bool
	}{
		{name: "deletes an existing file", path: "/a.txt", wantErr: false},
		{name: "error deleting a missing file", path: "/no.txt", wantErr: true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			s := NewMemoryStore()
			if err := s.CreateFile(stored); err != nil {
				t.Fatalf("setup failed: %v", err)
			}

			err := s.DeleteFile("ana", tc.path)
			if (err != nil) != tc.wantErr {
				t.Fatalf("DeleteFile() error = %v, wantErr = %v", err, tc.wantErr)
			}

			if !tc.wantErr {
				if _, err := s.GetFile("ana", tc.path); err == nil {
					t.Fatalf("file %q should have been deleted", tc.path)
				}
			}
		})
	}
}

func TestListFiles(t *testing.T) {
	seed := []FileMetadata{
		{Name: "a.txt", Path: "/home/ana/a.txt", Owner: "ana"},
		{Name: "b.txt", Path: "/home/ana/b.txt", Owner: "ana"},
		{Name: "c.txt", Path: "/home/bob/c.txt", Owner: "bob"},
	}

	tests := []struct {
		name      string
		owner     string
		dirPath   string
		wantCount int
	}{
		{name: "lists files in a directory", owner: "ana", dirPath: "/home/ana", wantCount: 2},
		{name: "lists files from another directory", owner: "bob", dirPath: "/home/bob", wantCount: 1},
		{name: "does not see another user's files", owner: "ana", dirPath: "/home/bob", wantCount: 0},
		{name: "directory without files", owner: "ana", dirPath: "/tmp", wantCount: 0},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			s := NewMemoryStore()
			for _, f := range seed {
				if err := s.CreateFile(f); err != nil {
					t.Fatalf("setup failed: %v", err)
				}
			}

			got, err := s.ListFiles(tc.owner, tc.dirPath)
			if err != nil {
				t.Fatalf("ListFiles() error unexpected: %v", err)
			}

			if len(got) != tc.wantCount {
				t.Fatalf("ListFiles() returned %d files, expected %d", len(got), tc.wantCount)
			}
		})
	}
}

func TestSaveAndGetBlockLocation(t *testing.T) {
	block := BlockMetadata{BlockID: "blk-1", DataNodes: []string{"dn-1", "dn-2"}, SHA256: "abc"}

	tests := []struct {
		name    string
		blockID string
		wantErr bool
	}{
		{name: "gets a stored block", blockID: "blk-1", wantErr: false},
		{name: "error when the block does not exist", blockID: "blk-x", wantErr: true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			s := NewMemoryStore()
			if err := s.SaveBlockLocation(block); err != nil {
				t.Fatalf("setup failed: %v", err)
			}

			got, err := s.GetBlockLocation(tc.blockID)
			if (err != nil) != tc.wantErr {
				t.Fatalf("GetBlockLocation() error = %v, wantErr = %v", err, tc.wantErr)
			}

			if !tc.wantErr && len(got.DataNodes) != len(block.DataNodes) {
				t.Fatalf("GetBlockLocation() returned %d DataNodes, expected %d", len(got.DataNodes), len(block.DataNodes))
			}
		})
	}
}

func TestSaveBlockLocationUpsert(t *testing.T) {
	s := NewMemoryStore()

	if err := s.SaveBlockLocation(BlockMetadata{BlockID: "blk-1", DataNodes: []string{"dn-1"}}); err != nil {
		t.Fatalf("first save failed: %v", err)
	}

	if err := s.SaveBlockLocation(BlockMetadata{BlockID: "blk-1", DataNodes: []string{"dn-1", "dn-2"}}); err != nil {
		t.Fatalf("second save (upsert) failed: %v", err)
	}

	got, err := s.GetBlockLocation("blk-1")
	if err != nil {
		t.Fatalf("GetBlockLocation() error unexpected: %v", err)
	}

	if len(got.DataNodes) != 2 {
		t.Fatalf("upsert did not update the block: datanodes = %d, expected 2", len(got.DataNodes))
	}
}

func TestRegisterDataNode(t *testing.T) {
	s := NewMemoryStore()

	if err := s.RegisterDataNode(DataNodeInfo{ID: "dn-1", Address: "127.0.0.1:9000"}); err != nil {
		t.Fatalf("new registration failed: %v", err)
	}

	// Un re-registro con el mismo ID sobrescribe en vez de fallar.
	if err := s.RegisterDataNode(DataNodeInfo{ID: "dn-1", Address: "127.0.0.1:9999"}); err != nil {
		t.Fatalf("re-registration failed: %v", err)
	}

	nodes, _ := s.ListAliveDataNodes()
	if len(nodes) != 1 {
		t.Fatalf("expected 1 datanode, got %d", len(nodes))
	}
	if nodes[0].Address != "127.0.0.1:9999" {
		t.Fatalf("el re-registro no sobrescribió la dirección: %q", nodes[0].Address)
	}
}

func TestRegisterDataNodeMarksAlive(t *testing.T) {
	s := NewMemoryStore()

	// Se registra con Alive=false a propósito; el store debe forzarlo a true.
	if err := s.RegisterDataNode(DataNodeInfo{ID: "dn-1", Alive: false}); err != nil {
		t.Fatalf("RegisterDataNode() error unexpected: %v", err)
	}

	alive, err := s.ListAliveDataNodes()
	if err != nil {
		t.Fatalf("ListAliveDataNodes() error unexpected: %v", err)
	}

	if len(alive) != 1 {
		t.Fatalf("expected 1 alive DataNode, got %d", len(alive))
	}

	if alive[0].LastHeartbeat.IsZero() {
		t.Fatalf("RegisterDataNode() should have set LastHeartbeat")
	}
}

func TestUpdateHeartbeat(t *testing.T) {
	tests := []struct {
		name    string
		id      string
		wantErr bool
	}{
		{name: "actualiza un datanode existente", id: "dn-1", wantErr: false},
		{name: "error si el datanode no existe", id: "dn-x", wantErr: true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			s := NewMemoryStore()
			if err := s.RegisterDataNode(DataNodeInfo{ID: "dn-1"}); err != nil {
				t.Fatalf("setup failed: %v", err)
			}

			err := s.UpdateHeartbeat(tc.id, 500)
			if (err != nil) != tc.wantErr {
				t.Fatalf("UpdateHeartbeat() error = %v, wantErr = %v", err, tc.wantErr)
			}

			if !tc.wantErr {
				nodes, _ := s.ListAliveDataNodes()
				if len(nodes) != 1 || nodes[0].FreeBytes != 500 {
					t.Fatalf("UpdateHeartbeat() no actualizó FreeBytes a 500")
				}
			}
		})
	}
}

func TestListAliveDataNodes(t *testing.T) {
	s := NewMemoryStore()

	if err := s.RegisterDataNode(DataNodeInfo{ID: "dn-1"}); err != nil {
		t.Fatalf("setup failed: %v", err)
	}
	if err := s.RegisterDataNode(DataNodeInfo{ID: "dn-2"}); err != nil {
		t.Fatalf("setup failed: %v", err)
	}

	nodes, err := s.ListAliveDataNodes()
	if err != nil {
		t.Fatalf("ListAliveDataNodes() error unexpected: %v", err)
	}

	if len(nodes) != 2 {
		t.Fatalf("expected 2 alive DataNodes, got %d", len(nodes))
	}
}

func TestCreateUser(t *testing.T) {
	existing := UserInfo{Username: "ana", PasswordHash: "hash"}

	tests := []struct {
		name    string
		input   UserInfo
		wantErr bool
	}{
		{name: "creates a new user", input: UserInfo{Username: "bob", PasswordHash: "hash"}, wantErr: false},
		{name: "rechaza un username duplicado", input: existing, wantErr: true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			s := NewMemoryStore()
			if err := s.CreateUser(existing); err != nil {
				t.Fatalf("setup failed: %v", err)
			}

			err := s.CreateUser(tc.input)
			if (err != nil) != tc.wantErr {
				t.Fatalf("CreateUser() error = %v, wantErr = %v", err, tc.wantErr)
			}
		})
	}
}

func TestGetUser(t *testing.T) {
	stored := UserInfo{Username: "ana", PasswordHash: "hash"}

	tests := []struct {
		name     string
		username string
		wantErr  bool
	}{
		{name: "gets an existing user", username: "ana", wantErr: false},
		{name: "error when the user does not exist", username: "nadie", wantErr: true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			s := NewMemoryStore()
			if err := s.CreateUser(stored); err != nil {
				t.Fatalf("setup failed: %v", err)
			}

			got, err := s.GetUser(tc.username)
			if (err != nil) != tc.wantErr {
				t.Fatalf("GetUser() error = %v, wantErr = %v", err, tc.wantErr)
			}

			if !tc.wantErr && got.Username != stored.Username {
				t.Fatalf("GetUser() returned %q, expected %q", got.Username, stored.Username)
			}
		})
	}
}

func TestUpdateQuotaUsed(t *testing.T) {
	tests := []struct {
		name     string
		username string
		delta    int64
		wantUsed int64
		wantErr  bool
	}{
		{name: "increase quota", username: "ana", delta: 100, wantUsed: 100, wantErr: false},
		{name: "decrease quota", username: "ana", delta: -40, wantUsed: -40, wantErr: false},
		{name: "error when the user does not exist", username: "nadie", delta: 10, wantErr: true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			s := NewMemoryStore()
			if err := s.CreateUser(UserInfo{Username: "ana", PasswordHash: "hash"}); err != nil {
				t.Fatalf("setup failed: %v", err)
			}

			err := s.UpdateQuotaUsed(tc.username, tc.delta)
			if (err != nil) != tc.wantErr {
				t.Fatalf("UpdateQuotaUsed() error = %v, wantErr = %v", err, tc.wantErr)
			}

			if !tc.wantErr {
				u, _ := s.GetUser(tc.username)
				if u.QuotaUsedBytes != tc.wantUsed {
					t.Fatalf("QuotaUsedBytes = %d, expected %d", u.QuotaUsedBytes, tc.wantUsed)
				}
			}
		})
	}
}

func TestListDataNodes(t *testing.T) {
	s := NewMemoryStore()

	if err := s.RegisterDataNode(DataNodeInfo{ID: "dn-1"}); err != nil {
		t.Fatalf("setup failed: %v", err)
	}
	if err := s.RegisterDataNode(DataNodeInfo{ID: "dn-2"}); err != nil {
		t.Fatalf("setup failed: %v", err)
	}
	// Marcar uno como muerto no debe excluirlo de ListDataNodes.
	if err := s.SetDataNodeAlive("dn-2", false); err != nil {
		t.Fatalf("setup failed: %v", err)
	}

	all, err := s.ListDataNodes()
	if err != nil {
		t.Fatalf("ListDataNodes() error unexpected: %v", err)
	}
	if len(all) != 2 {
		t.Fatalf("ListDataNodes() returned %d, expected 2 (alive and dead)", len(all))
	}
}

func TestSetDataNodeAlive(t *testing.T) {
	s := NewMemoryStore()
	if err := s.RegisterDataNode(DataNodeInfo{ID: "dn-1"}); err != nil {
		t.Fatalf("setup failed: %v", err)
	}

	if err := s.SetDataNodeAlive("dn-1", false); err != nil {
		t.Fatalf("SetDataNodeAlive() error unexpected: %v", err)
	}

	alive, _ := s.ListAliveDataNodes()
	if len(alive) != 0 {
		t.Fatalf("after marking it dead, there should be no alive nodes, got %d", len(alive))
	}

	if err := s.SetDataNodeAlive("missing", false); err != ErrDataNodeNotFound {
		t.Fatalf("error = %v, expected ErrDataNodeNotFound", err)
	}
}
