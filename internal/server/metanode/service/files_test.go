package service

import (
	"testing"

	"dfsha/internal/server/metanode/store"
)

// nuevoStoreConNodos crea un store con un usuario y la cantidad pedida de datanodes vivos
func nuevoStoreConNodos(t *testing.T, numNodos int) store.Store {
	t.Helper()
	s := store.NewMemoryStore()

	if err := s.CreateUser(store.UserInfo{Username: "ana"}); err != nil {
		t.Fatalf("preparación falló: %v", err)
	}
	for i := 0; i < numNodos; i++ {
		id := string(rune('a' + i))
		if err := s.RegisterDataNode(store.DataNodeInfo{ID: id, Address: id + ":9000"}); err != nil {
			t.Fatalf("preparación falló: %v", err)
		}
	}
	return s
}

func TestInitPutValido(t *testing.T) {
	s := nuevoStoreConNodos(t, 3)

	// 3MB con bloques de 1MB => 3 bloques
	placements, err := InitPut(s, "ana", "/home/ana/a.txt", 3*BlockSizeBytes)
	if err != nil {
		t.Fatalf("InitPut falló: %v", err)
	}
	if len(placements) != 3 {
		t.Fatalf("se esperaban 3 bloques, hay %d", len(placements))
	}
	for _, p := range placements {
		if len(p.DataNodes) != ReplicationFactor {
			t.Fatalf("bloque %s tiene %d réplicas, se esperaban %d", p.BlockID, len(p.DataNodes), ReplicationFactor)
		}
	}
}

func TestInitPutRedondeaHaciaArriba(t *testing.T) {
	s := nuevoStoreConNodos(t, 3)

	// 1 byte sobre un bloque entero => 2 bloques
	placements, err := InitPut(s, "ana", "/a.txt", BlockSizeBytes+1)
	if err != nil {
		t.Fatalf("InitPut falló: %v", err)
	}
	if len(placements) != 2 {
		t.Fatalf("se esperaban 2 bloques, hay %d", len(placements))
	}
}

func TestInitPutCuotaExcedida(t *testing.T) {
	s := nuevoStoreConNodos(t, 3)

	if _, err := InitPut(s, "ana", "/grande.bin", MaxUserQuotaBytes+1); err != ErrQuotaExceeded {
		t.Fatalf("error = %v, se esperaba ErrQuotaExceeded", err)
	}
}

func TestInitPutSinDatanodes(t *testing.T) {
	s := nuevoStoreConNodos(t, 2) // menos que ReplicationFactor

	if _, err := InitPut(s, "ana", "/a.txt", BlockSizeBytes); err == nil {
		t.Fatalf("se esperaba error por falta de datanodes")
	}
}

func TestCompletePutCobraCuota(t *testing.T) {
	s := nuevoStoreConNodos(t, 3)
	size := int64(2 * BlockSizeBytes)

	if _, err := InitPut(s, "ana", "/a.txt", size); err != nil {
		t.Fatalf("preparación falló: %v", err)
	}
	if err := CompletePut(s, "ana", "/a.txt", "hash-abc"); err != nil {
		t.Fatalf("CompletePut falló: %v", err)
	}

	u, _ := s.GetUser("ana")
	if u.QuotaUsedBytes != size {
		t.Fatalf("cuota usada = %d, se esperaba %d", u.QuotaUsedBytes, size)
	}
	f, _ := s.GetFile("/a.txt")
	if f.SHA256 != "hash-abc" {
		t.Fatalf("hash = %q, se esperaba \"hash-abc\"", f.SHA256)
	}
}

func TestResolveGetValido(t *testing.T) {
	s := nuevoStoreConNodos(t, 3)
	if _, err := InitPut(s, "ana", "/a.txt", 2*BlockSizeBytes); err != nil {
		t.Fatalf("preparación falló: %v", err)
	}

	blocks, _, err := ResolveGet(s, "ana", "/a.txt")
	if err != nil {
		t.Fatalf("ResolveGet falló: %v", err)
	}
	if len(blocks) != 2 {
		t.Fatalf("se esperaban 2 bloques, hay %d", len(blocks))
	}
}

func TestResolveGetAjeno(t *testing.T) {
	s := nuevoStoreConNodos(t, 3)
	if _, err := InitPut(s, "ana", "/a.txt", BlockSizeBytes); err != nil {
		t.Fatalf("preparación falló: %v", err)
	}
	if err := s.CreateUser(store.UserInfo{Username: "bob"}); err != nil {
		t.Fatalf("preparación falló: %v", err)
	}

	if _, _, err := ResolveGet(s, "bob", "/a.txt"); err != ErrForbidden {
		t.Fatalf("error = %v, se esperaba ErrForbidden", err)
	}
}

func TestListDirSoloDelUsuario(t *testing.T) {
	s := nuevoStoreConNodos(t, 3)
	if err := s.CreateUser(store.UserInfo{Username: "bob"}); err != nil {
		t.Fatalf("preparación falló: %v", err)
	}
	if _, err := InitPut(s, "ana", "/home/ana/a.txt", BlockSizeBytes); err != nil {
		t.Fatalf("preparación falló: %v", err)
	}
	if _, err := InitPut(s, "bob", "/home/ana/b.txt", BlockSizeBytes); err != nil {
		t.Fatalf("preparación falló: %v", err)
	}

	files, err := ListDir(s, "ana", "/home/ana")
	if err != nil {
		t.Fatalf("ListDir falló: %v", err)
	}
	if len(files) != 1 || files[0].Owner != "ana" {
		t.Fatalf("ListDir debió devolver solo el archivo de ana, obtuvo %d", len(files))
	}
}

func TestDeleteFileLiberaCuota(t *testing.T) {
	s := nuevoStoreConNodos(t, 3)
	size := int64(2 * BlockSizeBytes)
	if _, err := InitPut(s, "ana", "/a.txt", size); err != nil {
		t.Fatalf("preparación falló: %v", err)
	}
	if err := CompletePut(s, "ana", "/a.txt", "hash"); err != nil {
		t.Fatalf("preparación falló: %v", err)
	}

	if err := DeleteFile(s, "ana", "/a.txt"); err != nil {
		t.Fatalf("DeleteFile falló: %v", err)
	}

	u, _ := s.GetUser("ana")
	if u.QuotaUsedBytes != 0 {
		t.Fatalf("cuota usada = %d, se esperaba 0 tras borrar", u.QuotaUsedBytes)
	}
	if _, err := s.GetFile("/a.txt"); err == nil {
		t.Fatalf("el archivo debió borrarse")
	}
}

func TestDeleteFileAjeno(t *testing.T) {
	s := nuevoStoreConNodos(t, 3)
	if _, err := InitPut(s, "ana", "/a.txt", BlockSizeBytes); err != nil {
		t.Fatalf("preparación falló: %v", err)
	}
	if err := s.CreateUser(store.UserInfo{Username: "bob"}); err != nil {
		t.Fatalf("preparación falló: %v", err)
	}

	if err := DeleteFile(s, "bob", "/a.txt"); err != ErrForbidden {
		t.Fatalf("error = %v, se esperaba ErrForbidden", err)
	}
}
