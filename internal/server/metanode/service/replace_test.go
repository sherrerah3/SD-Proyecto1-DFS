package service

import (
	"strings"
	"testing"

	"dfsha/internal/server/metanode/store"
)

// hashNuevo es otro SHA256 válido, distinto de hashValido
var hashNuevo = strings.Repeat("a", 64)

// quorumPara reporta cada bloque desde dos DataNodes con el mismo hash
func quorumPara(t *testing.T, s store.Store, blockIDs []string) {
	t.Helper()
	for _, id := range blockIDs {
		for _, dn := range []string{"dn-a", "dn-b"} {
			if err := s.ReportBlock(id, dn, hashNuevo); err != nil {
				t.Fatalf("preparación falló (ReportBlock): %v", err)
			}
		}
	}
}

func quotaDe(t *testing.T, s store.Store, user string) int64 {
	t.Helper()
	u, err := s.GetUser(user)
	if err != nil {
		t.Fatalf("GetUser falló: %v", err)
	}
	return u.QuotaUsedBytes
}

func TestInitReplaceDejaLegibleLaVersionActual(t *testing.T) {
	s := nuevoStoreConNodos(t, 3)
	subir(t, s, "ana", "/a.txt") // 1 bloque

	placements, err := InitReplace(s, "ana", "/a.txt", 2*BlockSizeBytes, hashValido)
	if err != nil {
		t.Fatalf("InitReplace falló: %v", err)
	}
	if len(placements) != 2 {
		t.Fatalf("se esperaban 2 bloques nuevos, hay %d", len(placements))
	}

	// La versión actual sigue intacta y legible.
	blocks, hash, size, err := ResolveGet(s, "ana", "/a.txt")
	if err != nil {
		t.Fatalf("ResolveGet falló: %v", err)
	}
	if len(blocks) != 1 || hash != hashValido || size != BlockSizeBytes {
		t.Fatalf("la versión actual cambió: %d bloques, hash %q, size %d", len(blocks), hash, size)
	}

	// Se reservó solo lo que crece (1 bloque más).
	if got := quotaDe(t, s, "ana"); got != 2*BlockSizeBytes {
		t.Fatalf("cuota = %d, se esperaba %d", got, 2*BlockSizeBytes)
	}
}

func TestInitReplaceVersionVieja(t *testing.T) {
	s := nuevoStoreConNodos(t, 3)
	subir(t, s, "ana", "/a.txt")
	antes := quotaDe(t, s, "ana")

	_, err := InitReplace(s, "ana", "/a.txt", 2*BlockSizeBytes, hashNuevo)
	if err != ErrStaleVersion {
		t.Fatalf("error = %v, se esperaba ErrStaleVersion", err)
	}
	if got := quotaDe(t, s, "ana"); got != antes {
		t.Fatalf("la cuota no debía cambiar: %d -> %d", antes, got)
	}
	f, _ := s.GetFile("ana", "/a.txt")
	if f.Pending != nil {
		t.Fatalf("no debía quedar versión pendiente")
	}
}

func TestInitReplaceInexistenteIncompletoOAjeno(t *testing.T) {
	s := nuevoStoreConNodos(t, 3)
	if err := s.CreateUser(store.UserInfo{Username: "bob"}); err != nil {
		t.Fatalf("preparación falló: %v", err)
	}

	if _, err := InitReplace(s, "ana", "/no.txt", BlockSizeBytes, hashValido); err != ErrNotFound {
		t.Fatalf("inexistente: error = %v, se esperaba ErrNotFound", err)
	}

	// Subida sin completar: no hay versión que reemplazar.
	if _, err := InitPut(s, "ana", "/a.txt", BlockSizeBytes); err != nil {
		t.Fatalf("preparación falló: %v", err)
	}
	if _, err := InitReplace(s, "ana", "/a.txt", BlockSizeBytes, hashValido); err != ErrNotFound {
		t.Fatalf("incompleto: error = %v, se esperaba ErrNotFound", err)
	}

	subirListo(t, s, "ana", "/a.txt")
	if _, err := InitReplace(s, "bob", "/a.txt", BlockSizeBytes, hashValido); err != ErrNotFound {
		t.Fatalf("ajeno: error = %v, se esperaba ErrNotFound", err)
	}
}

func TestInitReplaceTamanoInvalido(t *testing.T) {
	s := nuevoStoreConNodos(t, 3)
	subir(t, s, "ana", "/a.txt")

	if _, err := InitReplace(s, "ana", "/a.txt", 0, hashValido); err != ErrInvalidSize {
		t.Fatalf("error = %v, se esperaba ErrInvalidSize", err)
	}
}

func TestInitReplaceCuotaExcedida(t *testing.T) {
	s := nuevoStoreConNodos(t, 3)
	subir(t, s, "ana", "/a.txt")
	antes := quotaDe(t, s, "ana")

	if _, err := InitReplace(s, "ana", "/a.txt", MaxUserQuotaBytes+1, hashValido); err != ErrQuotaExceeded {
		t.Fatalf("error = %v, se esperaba ErrQuotaExceeded", err)
	}
	if got := quotaDe(t, s, "ana"); got != antes {
		t.Fatalf("la cuota no debía cambiar: %d -> %d", antes, got)
	}
}

func TestCompletePutReemplazaLaVersion(t *testing.T) {
	s := nuevoStoreConNodos(t, 3)
	subir(t, s, "ana", "/a.txt")
	viejo, _ := s.GetFile("ana", "/a.txt")

	placements, err := InitReplace(s, "ana", "/a.txt", 2*BlockSizeBytes, hashValido)
	if err != nil {
		t.Fatalf("InitReplace falló: %v", err)
	}
	nuevos := blockIDsOf(placements)

	// Sin quórum no se confirma y el original queda intacto.
	if err := CompletePut(s, "ana", "/a.txt", hashNuevo); err != ErrQuorumNotMet {
		t.Fatalf("error = %v, se esperaba ErrQuorumNotMet", err)
	}
	if f, _ := s.GetFile("ana", "/a.txt"); f.SHA256 != hashValido {
		t.Fatalf("el original debía seguir igual, hash %q", f.SHA256)
	}

	quorumPara(t, s, nuevos)
	if err := CompletePut(s, "ana", "/a.txt", hashNuevo); err != nil {
		t.Fatalf("CompletePut falló: %v", err)
	}

	f, _ := s.GetFile("ana", "/a.txt")
	if f.SHA256 != hashNuevo || f.SizeBytes != 2*BlockSizeBytes || len(f.BlockIDs) != 2 || f.Pending != nil {
		t.Fatalf("archivo no reemplazado: %+v", f)
	}
	if got := quotaDe(t, s, "ana"); got != 2*BlockSizeBytes {
		t.Fatalf("cuota = %d, se esperaba %d", got, 2*BlockSizeBytes)
	}

	// Los bloques viejos se olvidan y se ordena borrarlos en cada DataNode.
	if _, err := s.GetBlockLocation(viejo.BlockIDs[0]); err == nil {
		t.Fatalf("el bloque viejo debía olvidarse")
	}
	pend, _ := s.DequeueBlockDeletions("a")
	if len(pend) != 1 || pend[0] != viejo.BlockIDs[0] {
		t.Fatalf("borrados pendientes = %v, se esperaba el bloque viejo", pend)
	}

	// Un segundo CompletePut ya no tiene nada que confirmar.
	if err := CompletePut(s, "ana", "/a.txt", hashNuevo); err != ErrAlreadyCompleted {
		t.Fatalf("error = %v, se esperaba ErrAlreadyCompleted", err)
	}
}

func TestCompletePutReemplazoMasChicoLiberaCuota(t *testing.T) {
	s := nuevoStoreConNodos(t, 3)
	if _, err := InitPut(s, "ana", "/a.txt", 2*BlockSizeBytes); err != nil {
		t.Fatalf("preparación falló: %v", err)
	}
	subirListo(t, s, "ana", "/a.txt")

	placements, err := InitReplace(s, "ana", "/a.txt", BlockSizeBytes, hashValido)
	if err != nil {
		t.Fatalf("InitReplace falló: %v", err)
	}
	if got := quotaDe(t, s, "ana"); got != 2*BlockSizeBytes {
		t.Fatalf("al achicar no se reserva nada, cuota = %d", got)
	}

	quorumPara(t, s, blockIDsOf(placements))
	if err := CompletePut(s, "ana", "/a.txt", hashNuevo); err != nil {
		t.Fatalf("CompletePut falló: %v", err)
	}
	if got := quotaDe(t, s, "ana"); got != BlockSizeBytes {
		t.Fatalf("cuota = %d, se esperaba %d", got, BlockSizeBytes)
	}
}

func TestInitReplaceDescartaElReemplazoAbandonado(t *testing.T) {
	s := nuevoStoreConNodos(t, 3)
	subir(t, s, "ana", "/a.txt")

	primero, err := InitReplace(s, "ana", "/a.txt", 3*BlockSizeBytes, hashValido)
	if err != nil {
		t.Fatalf("primer InitReplace falló: %v", err)
	}
	if _, err := InitReplace(s, "ana", "/a.txt", 2*BlockSizeBytes, hashValido); err != nil {
		t.Fatalf("segundo InitReplace falló: %v", err)
	}

	// Solo cuenta la reserva del segundo (original 1 + crecimiento 1).
	if got := quotaDe(t, s, "ana"); got != 2*BlockSizeBytes {
		t.Fatalf("cuota = %d, se esperaba %d", got, 2*BlockSizeBytes)
	}
	for _, p := range primero {
		if _, err := s.GetBlockLocation(p.BlockID); err == nil {
			t.Fatalf("el bloque %s del reemplazo abandonado debía descartarse", p.BlockID)
		}
	}
}

func TestDeleteFileConReemplazoPendiente(t *testing.T) {
	s := nuevoStoreConNodos(t, 3)
	subir(t, s, "ana", "/a.txt")
	placements, err := InitReplace(s, "ana", "/a.txt", 3*BlockSizeBytes, hashValido)
	if err != nil {
		t.Fatalf("InitReplace falló: %v", err)
	}

	if err := DeleteFile(s, "ana", "/a.txt"); err != nil {
		t.Fatalf("DeleteFile falló: %v", err)
	}
	if got := quotaDe(t, s, "ana"); got != 0 {
		t.Fatalf("cuota = %d, se esperaba 0", got)
	}
	for _, p := range placements {
		if _, err := s.GetBlockLocation(p.BlockID); err == nil {
			t.Fatalf("el bloque pendiente %s debía descartarse", p.BlockID)
		}
	}
	// 1 bloque actual + 3 pendientes por cada uno de los 3 DataNodes.
	if pend, _ := s.DequeueBlockDeletions("a"); len(pend) != 4 {
		t.Fatalf("borrados pendientes = %d, se esperaban 4", len(pend))
	}
}
