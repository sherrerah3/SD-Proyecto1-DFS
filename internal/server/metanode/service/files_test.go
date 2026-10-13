package service

import (
	"testing"

	"dfsha/internal/server/metanode/store"
)

// hashValido es un SHA256 en hex (64 caracteres) para los tests de CompletePut
const hashValido = "0000000000000000000000000000000000000000000000000000000000000000"

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

func TestInitPutReservaCuota(t *testing.T) {
	s := nuevoStoreConNodos(t, 3)
	size := int64(2 * BlockSizeBytes)

	if _, err := InitPut(s, "ana", "/a.txt", size); err != nil {
		t.Fatalf("InitPut falló: %v", err)
	}

	// La cuota se reserva en InitPut, antes de completar la subida.
	u, _ := s.GetUser("ana")
	if u.QuotaUsedBytes != size {
		t.Fatalf("cuota reservada = %d, se esperaba %d", u.QuotaUsedBytes, size)
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

func TestInitPutTamanoInvalido(t *testing.T) {
	s := nuevoStoreConNodos(t, 3)

	if _, err := InitPut(s, "ana", "/a.txt", 0); err != ErrInvalidSize {
		t.Fatalf("error = %v, se esperaba ErrInvalidSize", err)
	}
	if _, err := InitPut(s, "ana", "/a.txt", -5); err != ErrInvalidSize {
		t.Fatalf("error = %v, se esperaba ErrInvalidSize", err)
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

func TestCompletePutGuardaHash(t *testing.T) {
	s := nuevoStoreConNodos(t, 3)
	if _, err := InitPut(s, "ana", "/a.txt", 2*BlockSizeBytes); err != nil {
		t.Fatalf("preparación falló: %v", err)
	}

	if err := CompletePut(s, "ana", "/a.txt", hashValido); err != nil {
		t.Fatalf("CompletePut falló: %v", err)
	}

	f, _ := s.GetFile("/a.txt")
	if f.SHA256 != hashValido {
		t.Fatalf("hash = %q, se esperaba %q", f.SHA256, hashValido)
	}
}

func TestCompletePutHashInvalido(t *testing.T) {
	s := nuevoStoreConNodos(t, 3)
	if _, err := InitPut(s, "ana", "/a.txt", BlockSizeBytes); err != nil {
		t.Fatalf("preparación falló: %v", err)
	}

	if err := CompletePut(s, "ana", "/a.txt", "no-es-un-hash"); err != ErrInvalidHash {
		t.Fatalf("error = %v, se esperaba ErrInvalidHash", err)
	}
}

func TestCompletePutDosVeces(t *testing.T) {
	s := nuevoStoreConNodos(t, 3)
	if _, err := InitPut(s, "ana", "/a.txt", BlockSizeBytes); err != nil {
		t.Fatalf("preparación falló: %v", err)
	}
	if err := CompletePut(s, "ana", "/a.txt", hashValido); err != nil {
		t.Fatalf("primer CompletePut falló: %v", err)
	}

	if err := CompletePut(s, "ana", "/a.txt", hashValido); err != ErrAlreadyCompleted {
		t.Fatalf("error = %v, se esperaba ErrAlreadyCompleted", err)
	}
}

func TestResolveGetValido(t *testing.T) {
	s := nuevoStoreConNodos(t, 3)
	if _, err := InitPut(s, "ana", "/a.txt", 2*BlockSizeBytes); err != nil {
		t.Fatalf("preparación falló: %v", err)
	}
	if err := CompletePut(s, "ana", "/a.txt", hashValido); err != nil {
		t.Fatalf("preparación falló: %v", err)
	}

	blocks, hash, err := ResolveGet(s, "ana", "/a.txt")
	if err != nil {
		t.Fatalf("ResolveGet falló: %v", err)
	}
	if len(blocks) != 2 {
		t.Fatalf("se esperaban 2 bloques, hay %d", len(blocks))
	}
	if hash != hashValido {
		t.Fatalf("hash = %q, se esperaba %q", hash, hashValido)
	}
}

func TestResolveGetSubidaIncompleta(t *testing.T) {
	s := nuevoStoreConNodos(t, 3)
	// InitPut sin CompletePut: el archivo aún no tiene hash.
	if _, err := InitPut(s, "ana", "/a.txt", BlockSizeBytes); err != nil {
		t.Fatalf("preparación falló: %v", err)
	}

	if _, _, err := ResolveGet(s, "ana", "/a.txt"); err != ErrNotFound {
		t.Fatalf("error = %v, se esperaba ErrNotFound", err)
	}
}

func TestResolveGetAjeno(t *testing.T) {
	s := nuevoStoreConNodos(t, 3)
	if _, err := InitPut(s, "ana", "/a.txt", BlockSizeBytes); err != nil {
		t.Fatalf("preparación falló: %v", err)
	}
	if err := CompletePut(s, "ana", "/a.txt", hashValido); err != nil {
		t.Fatalf("preparación falló: %v", err)
	}
	if err := s.CreateUser(store.UserInfo{Username: "bob"}); err != nil {
		t.Fatalf("preparación falló: %v", err)
	}

	// Un archivo ajeno se trata como inexistente, no se revela que existe.
	if _, _, err := ResolveGet(s, "bob", "/a.txt"); err != ErrNotFound {
		t.Fatalf("error = %v, se esperaba ErrNotFound", err)
	}
}

// subir hace InitPut + CompletePut para dejar el archivo listo en el store
func subir(t *testing.T, s store.Store, user, filePath string) {
	t.Helper()
	if _, err := InitPut(s, user, filePath, BlockSizeBytes); err != nil {
		t.Fatalf("preparación falló: %v", err)
	}
	if err := CompletePut(s, user, filePath, hashValido); err != nil {
		t.Fatalf("preparación falló: %v", err)
	}
}

func TestListDirSoloHijosDirectos(t *testing.T) {
	s := nuevoStoreConNodos(t, 3)

	subir(t, s, "ana", "/home/ana/a.txt")     // hijo directo: sí
	subir(t, s, "ana", "/home/ana/sub/b.txt") // nieto: no

	files, err := ListDir(s, "ana", "/home/ana")
	if err != nil {
		t.Fatalf("ListDir falló: %v", err)
	}
	if len(files) != 1 || files[0].Path != "/home/ana/a.txt" {
		t.Fatalf("ListDir debió devolver solo el hijo directo, obtuvo %d", len(files))
	}
}

func TestListDirSoloDelUsuario(t *testing.T) {
	s := nuevoStoreConNodos(t, 3)
	if err := s.CreateUser(store.UserInfo{Username: "bob"}); err != nil {
		t.Fatalf("preparación falló: %v", err)
	}

	subir(t, s, "ana", "/home/ana/a.txt")
	subir(t, s, "bob", "/home/ana/b.txt") // mismo dir, otro dueño

	files, err := ListDir(s, "ana", "/home/ana")
	if err != nil {
		t.Fatalf("ListDir falló: %v", err)
	}
	if len(files) != 1 || files[0].Owner != "ana" {
		t.Fatalf("ListDir debió devolver solo el archivo de ana, obtuvo %d", len(files))
	}
}

func TestListDirOmiteSubidasIncompletas(t *testing.T) {
	s := nuevoStoreConNodos(t, 3)

	subir(t, s, "ana", "/home/ana/listo.txt")
	// Esta queda sin CompletePut: no debe aparecer.
	if _, err := InitPut(s, "ana", "/home/ana/a-medias.txt", BlockSizeBytes); err != nil {
		t.Fatalf("preparación falló: %v", err)
	}

	files, err := ListDir(s, "ana", "/home/ana")
	if err != nil {
		t.Fatalf("ListDir falló: %v", err)
	}
	if len(files) != 1 || files[0].Path != "/home/ana/listo.txt" {
		t.Fatalf("ListDir debió omitir la subida incompleta, obtuvo %d", len(files))
	}
}

func TestDeleteFileLiberaCuota(t *testing.T) {
	s := nuevoStoreConNodos(t, 3)
	size := int64(2 * BlockSizeBytes)
	if _, err := InitPut(s, "ana", "/a.txt", size); err != nil {
		t.Fatalf("preparación falló: %v", err)
	}
	if err := CompletePut(s, "ana", "/a.txt", hashValido); err != nil {
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

	// Borrar un archivo ajeno se trata como inexistente.
	if err := DeleteFile(s, "bob", "/a.txt"); err != ErrNotFound {
		t.Fatalf("error = %v, se esperaba ErrNotFound", err)
	}
}
