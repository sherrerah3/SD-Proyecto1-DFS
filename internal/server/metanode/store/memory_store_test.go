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
			name:    "crea un archivo nuevo",
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
				t.Fatalf("preparación falló: %v", err)
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
		{name: "obtiene un archivo existente", path: "/a.txt", wantErr: false},
		{name: "error si no existe", path: "/no.txt", wantErr: true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			s := NewMemoryStore()
			if err := s.CreateFile(stored); err != nil {
				t.Fatalf("preparación falló: %v", err)
			}

			got, err := s.GetFile(tc.path)
			if (err != nil) != tc.wantErr {
				t.Fatalf("GetFile() error = %v, wantErr = %v", err, tc.wantErr)
			}

			if !tc.wantErr && got.Path != stored.Path {
				t.Fatalf("GetFile() devolvió path %q, se esperaba %q", got.Path, stored.Path)
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
		{name: "borra un archivo existente", path: "/a.txt", wantErr: false},
		{name: "error al borrar inexistente", path: "/no.txt", wantErr: true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			s := NewMemoryStore()
			if err := s.CreateFile(stored); err != nil {
				t.Fatalf("preparación falló: %v", err)
			}

			err := s.DeleteFile(tc.path)
			if (err != nil) != tc.wantErr {
				t.Fatalf("DeleteFile() error = %v, wantErr = %v", err, tc.wantErr)
			}

			if !tc.wantErr {
				if _, err := s.GetFile(tc.path); err == nil {
					t.Fatalf("el archivo %q debería haberse borrado", tc.path)
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
		dirPath   string
		wantCount int
	}{
		{name: "lista archivos de un directorio", dirPath: "/home/ana", wantCount: 2},
		{name: "lista archivos de otro directorio", dirPath: "/home/bob", wantCount: 1},
		{name: "directorio sin archivos", dirPath: "/tmp", wantCount: 0},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			s := NewMemoryStore()
			for _, f := range seed {
				if err := s.CreateFile(f); err != nil {
					t.Fatalf("preparación falló: %v", err)
				}
			}

			got, err := s.ListFiles(tc.dirPath)
			if err != nil {
				t.Fatalf("ListFiles() error inesperado: %v", err)
			}

			if len(got) != tc.wantCount {
				t.Fatalf("ListFiles() devolvió %d archivos, se esperaban %d", len(got), tc.wantCount)
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
		{name: "obtiene un bloque guardado", blockID: "blk-1", wantErr: false},
		{name: "error si el bloque no existe", blockID: "blk-x", wantErr: true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			s := NewMemoryStore()
			if err := s.SaveBlockLocation(block); err != nil {
				t.Fatalf("preparación falló: %v", err)
			}

			got, err := s.GetBlockLocation(tc.blockID)
			if (err != nil) != tc.wantErr {
				t.Fatalf("GetBlockLocation() error = %v, wantErr = %v", err, tc.wantErr)
			}

			if !tc.wantErr && len(got.DataNodes) != len(block.DataNodes) {
				t.Fatalf("GetBlockLocation() devolvió %d datanodes, se esperaban %d", len(got.DataNodes), len(block.DataNodes))
			}
		})
	}
}

func TestSaveBlockLocationUpsert(t *testing.T) {
	s := NewMemoryStore()

	if err := s.SaveBlockLocation(BlockMetadata{BlockID: "blk-1", DataNodes: []string{"dn-1"}}); err != nil {
		t.Fatalf("primer guardado falló: %v", err)
	}

	if err := s.SaveBlockLocation(BlockMetadata{BlockID: "blk-1", DataNodes: []string{"dn-1", "dn-2"}}); err != nil {
		t.Fatalf("segundo guardado (upsert) falló: %v", err)
	}

	got, err := s.GetBlockLocation("blk-1")
	if err != nil {
		t.Fatalf("GetBlockLocation() error inesperado: %v", err)
	}

	if len(got.DataNodes) != 2 {
		t.Fatalf("el upsert no actualizó el bloque: datanodes = %d, se esperaban 2", len(got.DataNodes))
	}
}

func TestRegisterDataNode(t *testing.T) {
	existing := DataNodeInfo{ID: "dn-1", Address: "127.0.0.1:9000"}

	tests := []struct {
		name    string
		input   DataNodeInfo
		wantErr bool
	}{
		{name: "registra un datanode nuevo", input: DataNodeInfo{ID: "dn-2", Address: "127.0.0.1:9001"}, wantErr: false},
		{name: "rechaza un ID duplicado", input: existing, wantErr: true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			s := NewMemoryStore()
			if err := s.RegisterDataNode(existing); err != nil {
				t.Fatalf("preparación falló: %v", err)
			}

			err := s.RegisterDataNode(tc.input)
			if (err != nil) != tc.wantErr {
				t.Fatalf("RegisterDataNode() error = %v, wantErr = %v", err, tc.wantErr)
			}
		})
	}
}

func TestRegisterDataNodeMarksAlive(t *testing.T) {
	s := NewMemoryStore()

	// Se registra con Alive=false a propósito; el store debe forzarlo a true.
	if err := s.RegisterDataNode(DataNodeInfo{ID: "dn-1", Alive: false}); err != nil {
		t.Fatalf("RegisterDataNode() error inesperado: %v", err)
	}

	alive, err := s.ListAliveDataNodes()
	if err != nil {
		t.Fatalf("ListAliveDataNodes() error inesperado: %v", err)
	}

	if len(alive) != 1 {
		t.Fatalf("se esperaba 1 datanode vivo, se obtuvieron %d", len(alive))
	}

	if alive[0].LastHeartbeat.IsZero() {
		t.Fatalf("RegisterDataNode() debió fijar LastHeartbeat")
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
				t.Fatalf("preparación falló: %v", err)
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
		t.Fatalf("preparación falló: %v", err)
	}
	if err := s.RegisterDataNode(DataNodeInfo{ID: "dn-2"}); err != nil {
		t.Fatalf("preparación falló: %v", err)
	}

	nodes, err := s.ListAliveDataNodes()
	if err != nil {
		t.Fatalf("ListAliveDataNodes() error inesperado: %v", err)
	}

	if len(nodes) != 2 {
		t.Fatalf("se esperaban 2 datanodes vivos, se obtuvieron %d", len(nodes))
	}
}

func TestCreateUser(t *testing.T) {
	existing := UserInfo{Username: "ana", PasswordHash: "hash"}

	tests := []struct {
		name    string
		input   UserInfo
		wantErr bool
	}{
		{name: "crea un usuario nuevo", input: UserInfo{Username: "bob", PasswordHash: "hash"}, wantErr: false},
		{name: "rechaza un username duplicado", input: existing, wantErr: true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			s := NewMemoryStore()
			if err := s.CreateUser(existing); err != nil {
				t.Fatalf("preparación falló: %v", err)
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
		{name: "obtiene un usuario existente", username: "ana", wantErr: false},
		{name: "error si el usuario no existe", username: "nadie", wantErr: true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			s := NewMemoryStore()
			if err := s.CreateUser(stored); err != nil {
				t.Fatalf("preparación falló: %v", err)
			}

			got, err := s.GetUser(tc.username)
			if (err != nil) != tc.wantErr {
				t.Fatalf("GetUser() error = %v, wantErr = %v", err, tc.wantErr)
			}

			if !tc.wantErr && got.Username != stored.Username {
				t.Fatalf("GetUser() devolvió %q, se esperaba %q", got.Username, stored.Username)
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
		{name: "suma cuota", username: "ana", delta: 100, wantUsed: 100, wantErr: false},
		{name: "resta cuota", username: "ana", delta: -40, wantUsed: -40, wantErr: false},
		{name: "error si el usuario no existe", username: "nadie", delta: 10, wantErr: true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			s := NewMemoryStore()
			if err := s.CreateUser(UserInfo{Username: "ana", PasswordHash: "hash"}); err != nil {
				t.Fatalf("preparación falló: %v", err)
			}

			err := s.UpdateQuotaUsed(tc.username, tc.delta)
			if (err != nil) != tc.wantErr {
				t.Fatalf("UpdateQuotaUsed() error = %v, wantErr = %v", err, tc.wantErr)
			}

			if !tc.wantErr {
				u, _ := s.GetUser(tc.username)
				if u.QuotaUsedBytes != tc.wantUsed {
					t.Fatalf("QuotaUsedBytes = %d, se esperaba %d", u.QuotaUsedBytes, tc.wantUsed)
				}
			}
		})
	}
}
