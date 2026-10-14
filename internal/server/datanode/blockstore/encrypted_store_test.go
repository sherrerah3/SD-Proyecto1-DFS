package blockstore

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"testing"

	"dfsha/internal/server/datanode/crypto"
)

func newEncryptedStore(t *testing.T) *EncryptedBlockStore {
	t.Helper()

	disk, err := NewDiskStore(t.TempDir())
	if err != nil {
		t.Fatalf("preparación falló: %v", err)
	}

	cipher, err := crypto.NewAESGCM(bytes.Repeat([]byte{0x02}, crypto.KeySize))
	if err != nil {
		t.Fatalf("preparación falló: %v", err)
	}

	return NewEncryptedBlockStore(disk, cipher)
}

func TestEncryptedWriteReadRoundTrip(t *testing.T) {
	s := newEncryptedStore(t)

	data := []byte("contenido en claro del bloque")

	info, err := s.Write("blk-1", bytes.NewReader(data))
	if err != nil {
		t.Fatalf("Write() error inesperado: %v", err)
	}

	// El hash y el tamaño reportados deben ser del contenido en claro.
	want := sha256.Sum256(data)
	if info.SHA256 != hex.EncodeToString(want[:]) {
		t.Fatalf("Write() sha256 = %q, se esperaba el del claro", info.SHA256)
	}

	if info.SizeBytes != int64(len(data)) {
		t.Fatalf("Write() size = %d, se esperaba %d", info.SizeBytes, len(data))
	}

	rc, _, err := s.Read("blk-1")
	if err != nil {
		t.Fatalf("Read() error inesperado: %v", err)
	}

	defer rc.Close()

	got, err := io.ReadAll(rc)
	if err != nil {
		t.Fatalf("ReadAll() error inesperado: %v", err)
	}

	if !bytes.Equal(got, data) {
		t.Fatalf("Read() devolvió %q, se esperaba %q", got, data)
	}
}

func TestEncryptedStoresCiphertextOnDisk(t *testing.T) {
	dir := t.TempDir()

	disk, err := NewDiskStore(dir)
	if err != nil {
		t.Fatalf("preparación falló: %v", err)
	}

	cipher, err := crypto.NewAESGCM(bytes.Repeat([]byte{0x03}, crypto.KeySize))
	if err != nil {
		t.Fatalf("preparación falló: %v", err)
	}

	enc := NewEncryptedBlockStore(disk, cipher)

	data := []byte("esto no debe quedar en claro en disco")
	if _, err := enc.Write("blk-1", bytes.NewReader(data)); err != nil {
		t.Fatalf("Write() error inesperado: %v", err)
	}

	// Leyendo directo del disco (sin descifrar) no debe aparecer el claro.
	rc, _, err := disk.Read("blk-1")
	if err != nil {
		t.Fatalf("Read() directo error inesperado: %v", err)
	}

	defer rc.Close()

	raw, err := io.ReadAll(rc)
	if err != nil {
		t.Fatalf("ReadAll() error inesperado: %v", err)
	}

	if bytes.Contains(raw, data) {
		t.Fatalf("el contenido en claro quedó visible en disco")
	}
}

func TestEncryptedVerifyMatchesPlaintextHash(t *testing.T) {
	s := newEncryptedStore(t)

	data := []byte("datos para verificar")
	info, err := s.Write("blk-1", bytes.NewReader(data))
	if err != nil {
		t.Fatalf("Write() error inesperado: %v", err)
	}

	report, exists, err := s.Verify("blk-1")
	if err != nil {
		t.Fatalf("Verify() error inesperado: %v", err)
	}

	if !exists {
		t.Fatalf("Verify() exists = false, se esperaba true")
	}

	// El hash de Verify debe coincidir con el que devolvió Write (claro).
	if report.SHA256 != info.SHA256 {
		t.Fatalf("Verify() sha256 = %q, se esperaba %q", report.SHA256, info.SHA256)
	}
}

func TestEncryptedVerifyMissingBlock(t *testing.T) {
	s := newEncryptedStore(t)

	if _, exists, err := s.Verify("no-existe"); err != nil || exists {
		t.Fatalf("Verify() de bloque inexistente: exists = %v, err = %v", exists, err)
	}
}

func TestEncryptedList(t *testing.T) {
	s := newEncryptedStore(t)

	for _, id := range []string{"blk-1", "blk-2"} {
		if _, err := s.Write(id, bytes.NewReader([]byte(id))); err != nil {
			t.Fatalf("preparación falló: %v", err)
		}
	}

	reports, err := s.List()
	if err != nil {
		t.Fatalf("List() error inesperado: %v", err)
	}

	if len(reports) != 2 {
		t.Fatalf("List() devolvió %d bloques, se esperaban 2", len(reports))
	}
}
