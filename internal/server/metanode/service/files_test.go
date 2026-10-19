package service

import (
	"path"
	"strings"
	"testing"

	"dfsha/internal/server/metanode/store"
)

// hashValido es un SHA256 en hex (64 caracteres) para los tests de CompletePut
const hashValido = "0000000000000000000000000000000000000000000000000000000000000000"

// alcanzarQuorum reporta cada bloque desde dos DataNodes con el mismo hash
func alcanzarQuorum(t *testing.T, s store.Store, user, filePath string) {
	t.Helper()
	f, err := s.GetFile(user, filePath)
	if err != nil {
		t.Fatalf("setup failed (GetFile): %v", err)
	}
	for _, blockID := range f.BlockIDs {
		if err := s.ReportBlock(blockID, "dn-a", hashValido); err != nil {
			t.Fatalf("setup failed (ReportBlock a): %v", err)
		}
		if err := s.ReportBlock(blockID, "dn-b", hashValido); err != nil {
			t.Fatalf("setup failed (ReportBlock b): %v", err)
		}
	}
}

// nuevoStoreConNodos crea un store con un usuario y la cantidad pedida de datanodes vivos
func nuevoStoreConNodos(t *testing.T, numNodos int) store.Store {
	t.Helper()
	s := store.NewMemoryStore()

	if err := s.CreateUser(store.UserInfo{Username: "ana"}); err != nil {
		t.Fatalf("setup failed: %v", err)
	}
	for i := 0; i < numNodos; i++ {
		id := string(rune('a' + i))
		if err := s.RegisterDataNode(store.DataNodeInfo{ID: id, Address: id + ":9000"}); err != nil {
			t.Fatalf("setup failed: %v", err)
		}
	}
	return s
}

func TestInitPutValido(t *testing.T) {
	s := nuevoStoreConNodos(t, 3)

	crearDirs(t, s, "ana", "/home/ana/a.txt")

	// 3MB con bloques de 1MB => 3 bloques
	placements, err := InitPut(s, "ana", "/home/ana/a.txt", 3*BlockSizeBytes)
	if err != nil {
		t.Fatalf("InitPut failed: %v", err)
	}
	if len(placements) != 3 {
		t.Fatalf("expected 3 blocks, got %d", len(placements))
	}
	for _, p := range placements {
		if len(p.DataNodes) != ReplicationFactor {
			t.Fatalf("block %s has %d replicas, expected %d", p.BlockID, len(p.DataNodes), ReplicationFactor)
		}
	}
}

func TestInitPutReservaCuota(t *testing.T) {
	s := nuevoStoreConNodos(t, 3)
	size := int64(2 * BlockSizeBytes)

	if _, err := InitPut(s, "ana", "/a.txt", size); err != nil {
		t.Fatalf("InitPut failed: %v", err)
	}

	// La cuota se reserva en InitPut, antes de completar la subida.
	u, _ := s.GetUser("ana")
	if u.QuotaUsedBytes != size {
		t.Fatalf("reserved quota = %d, expected %d", u.QuotaUsedBytes, size)
	}
}

func TestInitPutRedondeaHaciaArriba(t *testing.T) {
	s := nuevoStoreConNodos(t, 3)

	// 1 byte sobre un bloque entero => 2 bloques
	placements, err := InitPut(s, "ana", "/a.txt", BlockSizeBytes+1)
	if err != nil {
		t.Fatalf("InitPut failed: %v", err)
	}
	if len(placements) != 2 {
		t.Fatalf("expected 2 blocks, got %d", len(placements))
	}
}

func TestInitPutTamanoInvalido(t *testing.T) {
	s := nuevoStoreConNodos(t, 3)

	if _, err := InitPut(s, "ana", "/a.txt", 0); err != ErrInvalidSize {
		t.Fatalf("error = %v, expected ErrInvalidSize", err)
	}
	if _, err := InitPut(s, "ana", "/a.txt", -5); err != ErrInvalidSize {
		t.Fatalf("error = %v, expected ErrInvalidSize", err)
	}
}

func TestInitPutCuotaExcedida(t *testing.T) {
	s := nuevoStoreConNodos(t, 3)

	if _, err := InitPut(s, "ana", "/grande.bin", MaxUserQuotaBytes+1); err != ErrQuotaExceeded {
		t.Fatalf("error = %v, expected ErrQuotaExceeded", err)
	}
}

func TestInitPutSinDatanodes(t *testing.T) {
	s := nuevoStoreConNodos(t, 2) // menos que ReplicationFactor

	if _, err := InitPut(s, "ana", "/a.txt", BlockSizeBytes); err == nil {
		t.Fatalf("expected an error because no datanodes are available")
	}
}

func TestCompletePutGuardaHash(t *testing.T) {
	s := nuevoStoreConNodos(t, 3)
	if _, err := InitPut(s, "ana", "/a.txt", 2*BlockSizeBytes); err != nil {
		t.Fatalf("setup failed: %v", err)
	}
	alcanzarQuorum(t, s, "ana", "/a.txt")

	if err := CompletePut(s, "ana", "/a.txt", hashValido); err != nil {
		t.Fatalf("CompletePut failed: %v", err)
	}

	f, _ := s.GetFile("ana", "/a.txt")
	if f.SHA256 != hashValido {
		t.Fatalf("hash = %q, expected %q", f.SHA256, hashValido)
	}
}

func TestCompletePutSinQuorum(t *testing.T) {
	s := nuevoStoreConNodos(t, 3)
	if _, err := InitPut(s, "ana", "/a.txt", BlockSizeBytes); err != nil {
		t.Fatalf("setup failed: %v", err)
	}

	// Sin reportes no hay quórum
	if err := CompletePut(s, "ana", "/a.txt", hashValido); err != ErrQuorumNotMet {
		t.Fatalf("error = %v, expected ErrQuorumNotMet", err)
	}
}

func TestCompletePutHashInvalido(t *testing.T) {
	s := nuevoStoreConNodos(t, 3)
	if _, err := InitPut(s, "ana", "/a.txt", BlockSizeBytes); err != nil {
		t.Fatalf("setup failed: %v", err)
	}

	if err := CompletePut(s, "ana", "/a.txt", "no-es-un-hash"); err != ErrInvalidHash {
		t.Fatalf("error = %v, expected ErrInvalidHash", err)
	}
}

func TestCompletePutDosVeces(t *testing.T) {
	s := nuevoStoreConNodos(t, 3)
	if _, err := InitPut(s, "ana", "/a.txt", BlockSizeBytes); err != nil {
		t.Fatalf("setup failed: %v", err)
	}
	alcanzarQuorum(t, s, "ana", "/a.txt")
	if err := CompletePut(s, "ana", "/a.txt", hashValido); err != nil {
		t.Fatalf("primer CompletePut failed: %v", err)
	}

	if err := CompletePut(s, "ana", "/a.txt", hashValido); err != ErrAlreadyCompleted {
		t.Fatalf("error = %v, expected ErrAlreadyCompleted", err)
	}
}

func TestResolveGetValido(t *testing.T) {
	s := nuevoStoreConNodos(t, 3)
	if _, err := InitPut(s, "ana", "/a.txt", 2*BlockSizeBytes); err != nil {
		t.Fatalf("setup failed: %v", err)
	}
	subirListo(t, s, "ana", "/a.txt")

	blocks, hash, err := ResolveGet(s, "ana", "/a.txt")
	if err != nil {
		t.Fatalf("ResolveGet failed: %v", err)
	}
	if len(blocks) != 2 {
		t.Fatalf("expected 2 blocks, got %d", len(blocks))
	}
	if hash != hashValido {
		t.Fatalf("hash = %q, expected %q", hash, hashValido)
	}
}

func TestResolveGetReplicasVivasPrimero(t *testing.T) {
	s := nuevoStoreConNodos(t, 3)
	if _, err := InitPut(s, "ana", "/a.txt", BlockSizeBytes); err != nil {
		t.Fatalf("setup failed: %v", err)
	}
	subirListo(t, s, "ana", "/a.txt")

	// Se mata un nodo: su dirección debe quedar al final de la lista.
	nodes, _ := s.ListDataNodes()
	dead := nodes[0]
	if err := s.SetDataNodeAlive(dead.ID, false); err != nil {
		t.Fatalf("setup failed: %v", err)
	}

	blocks, _, err := ResolveGet(s, "ana", "/a.txt")
	if err != nil {
		t.Fatalf("ResolveGet failed: %v", err)
	}
	// El nodo muerto, si aparece, debe ser el último.
	addrs := blocks[0].DataNodes
	for i, a := range addrs {
		if a == dead.Address && i != len(addrs)-1 {
			t.Fatalf("dead replica %s should be last, found at position %d", a, i)
		}
	}
}

func TestResolveGetSubidaIncompleta(t *testing.T) {
	s := nuevoStoreConNodos(t, 3)
	// InitPut sin CompletePut: el archivo aún no tiene hash.
	if _, err := InitPut(s, "ana", "/a.txt", BlockSizeBytes); err != nil {
		t.Fatalf("setup failed: %v", err)
	}

	if _, _, err := ResolveGet(s, "ana", "/a.txt"); err != ErrNotFound {
		t.Fatalf("error = %v, expected ErrNotFound", err)
	}
}

func TestResolveGetAjeno(t *testing.T) {
	s := nuevoStoreConNodos(t, 3)
	if _, err := InitPut(s, "ana", "/a.txt", BlockSizeBytes); err != nil {
		t.Fatalf("setup failed: %v", err)
	}
	subirListo(t, s, "ana", "/a.txt")
	if err := s.CreateUser(store.UserInfo{Username: "bob"}); err != nil {
		t.Fatalf("setup failed: %v", err)
	}

	// Un archivo ajeno se trata como inexistente, no se revela que existe.
	if _, _, err := ResolveGet(s, "bob", "/a.txt"); err != ErrNotFound {
		t.Fatalf("error = %v, expected ErrNotFound", err)
	}
}

// crearDirs crea los directorios padre de filePath
func crearDirs(t *testing.T, s store.Store, user, filePath string) {
	t.Helper()
	dir := path.Dir(filePath)
	if dir == "/" || dir == "." {
		return
	}
	// Crear de la raíz hacia abajo.
	cur := ""
	for _, p := range strings.Split(strings.Trim(dir, "/"), "/") {
		if p == "" {
			continue
		}
		cur += "/" + p
		if !s.DirectoryExists(user, cur) {
			if err := s.CreateDirectory(user, cur); err != nil {
				t.Fatalf("setup failed (CreateDirectory %s): %v", cur, err)
			}
		}
	}
}

// subir hace InitPut + quórum + CompletePut para dejar el archivo listo.
func subir(t *testing.T, s store.Store, user, filePath string) {
	t.Helper()
	crearDirs(t, s, user, filePath)
	if _, err := InitPut(s, user, filePath, BlockSizeBytes); err != nil {
		t.Fatalf("setup failed: %v", err)
	}
	alcanzarQuorum(t, s, user, filePath)
	if err := CompletePut(s, user, filePath, hashValido); err != nil {
		t.Fatalf("setup failed: %v", err)
	}
}

// subirListo completa una subida ya iniciada
func subirListo(t *testing.T, s store.Store, user, filePath string) {
	t.Helper()
	alcanzarQuorum(t, s, user, filePath)
	if err := CompletePut(s, user, filePath, hashValido); err != nil {
		t.Fatalf("setup failed: %v", err)
	}
}

func TestListDirSoloHijosDirectos(t *testing.T) {
	s := nuevoStoreConNodos(t, 3)

	subir(t, s, "ana", "/home/ana/a.txt")     // hijo directo: sí
	subir(t, s, "ana", "/home/ana/sub/b.txt") // nieto: no

	files, err := ListDir(s, "ana", "/home/ana")
	if err != nil {
		t.Fatalf("ListDir failed: %v", err)
	}
	if len(files) != 1 || files[0].Path != "/home/ana/a.txt" {
		t.Fatalf("ListDir should have returned only the direct child, got %d", len(files))
	}
}

func TestListDirSoloDelUsuario(t *testing.T) {
	s := nuevoStoreConNodos(t, 3)
	if err := s.CreateUser(store.UserInfo{Username: "bob"}); err != nil {
		t.Fatalf("setup failed: %v", err)
	}

	subir(t, s, "ana", "/home/ana/a.txt")
	subir(t, s, "bob", "/home/ana/b.txt") // mismo dir, otro dueño

	files, err := ListDir(s, "ana", "/home/ana")
	if err != nil {
		t.Fatalf("ListDir failed: %v", err)
	}
	if len(files) != 1 || files[0].Owner != "ana" {
		t.Fatalf("ListDir should have returned only ana's file, got %d", len(files))
	}
}

func TestListDirOmiteSubidasIncompletas(t *testing.T) {
	s := nuevoStoreConNodos(t, 3)

	subir(t, s, "ana", "/home/ana/listo.txt")
	// Esta queda sin CompletePut: no debe aparecer.
	if _, err := InitPut(s, "ana", "/home/ana/a-medias.txt", BlockSizeBytes); err != nil {
		t.Fatalf("setup failed: %v", err)
	}

	files, err := ListDir(s, "ana", "/home/ana")
	if err != nil {
		t.Fatalf("ListDir failed: %v", err)
	}
	if len(files) != 1 || files[0].Path != "/home/ana/listo.txt" {
		t.Fatalf("ListDir should have omitted the incomplete upload, got %d", len(files))
	}
}

func TestDeleteFileLiberaCuota(t *testing.T) {
	s := nuevoStoreConNodos(t, 3)
	size := int64(2 * BlockSizeBytes)
	if _, err := InitPut(s, "ana", "/a.txt", size); err != nil {
		t.Fatalf("setup failed: %v", err)
	}
	subirListo(t, s, "ana", "/a.txt")

	if err := DeleteFile(s, "ana", "/a.txt"); err != nil {
		t.Fatalf("DeleteFile failed: %v", err)
	}

	u, _ := s.GetUser("ana")
	if u.QuotaUsedBytes != 0 {
		t.Fatalf("used quota = %d, expected 0 after deletion", u.QuotaUsedBytes)
	}
	if _, err := s.GetFile("ana", "/a.txt"); err == nil {
		t.Fatalf("the file should have been deleted")
	}
}

func TestDeleteFileAjeno(t *testing.T) {
	s := nuevoStoreConNodos(t, 3)
	if _, err := InitPut(s, "ana", "/a.txt", BlockSizeBytes); err != nil {
		t.Fatalf("setup failed: %v", err)
	}
	if err := s.CreateUser(store.UserInfo{Username: "bob"}); err != nil {
		t.Fatalf("setup failed: %v", err)
	}

	// Borrar un archivo ajeno se trata como inexistente.
	if err := DeleteFile(s, "bob", "/a.txt"); err != ErrNotFound {
		t.Fatalf("error = %v, expected ErrNotFound", err)
	}
}
