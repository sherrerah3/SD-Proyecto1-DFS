package blockstore

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"strings"
	"sync"
	"testing"
)

func hashOf(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

func TestWrite(t *testing.T) {
	tests := []struct {
		name    string
		blockID string
		data    []byte
		wantErr bool
	}{
		{name: "escribe un bloque nuevo", blockID: "blk-1", data: []byte("hola mundo"), wantErr: false},
		{name: "escribe un bloque vacío", blockID: "blk-vacio", data: []byte{}, wantErr: false},
		{name: "rechaza un block ID vacío", blockID: "", data: []byte("x"), wantErr: true},
		{name: "rechaza un block ID con separador", blockID: "a/b", data: []byte("x"), wantErr: true},
		{name: "rechaza un block ID con traversal", blockID: "..", data: []byte("x"), wantErr: true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			s, err := NewDiskStore(t.TempDir())
			if err != nil {
				t.Fatalf("preparación falló: %v", err)
			}

			info, err := s.Write(tc.blockID, bytes.NewReader(tc.data))
			if (err != nil) != tc.wantErr {
				t.Fatalf("Write() error = %v, wantErr = %v", err, tc.wantErr)
			}

			if tc.wantErr {
				return
			}

			if info.SizeBytes != int64(len(tc.data)) {
				t.Fatalf("Write() size = %d, se esperaba %d", info.SizeBytes, len(tc.data))
			}

			if info.SHA256 != hashOf(tc.data) {
				t.Fatalf("Write() sha256 = %q, se esperaba %q", info.SHA256, hashOf(tc.data))
			}
		})
	}
}

func TestReadRoundTrip(t *testing.T) {
	s, err := NewDiskStore(t.TempDir())
	if err != nil {
		t.Fatalf("preparación falló: %v", err)
	}

	data := []byte("contenido del bloque de prueba")
	if _, err := s.Write("blk-1", bytes.NewReader(data)); err != nil {
		t.Fatalf("Write() error inesperado: %v", err)
	}

	rc, info, err := s.Read("blk-1")
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

	if info.SizeBytes != int64(len(data)) {
		t.Fatalf("Read() size = %d, se esperaba %d", info.SizeBytes, len(data))
	}
}

func TestReadNotFound(t *testing.T) {
	s, err := NewDiskStore(t.TempDir())
	if err != nil {
		t.Fatalf("preparación falló: %v", err)
	}

	if _, _, err := s.Read("no-existe"); err == nil {
		t.Fatalf("Read() debió fallar para un bloque inexistente")
	}
}

func TestDelete(t *testing.T) {
	tests := []struct {
		name       string
		writeFirst bool
		wantOK     bool
	}{
		{name: "borra un bloque existente", writeFirst: true, wantOK: true},
		{name: "borrar inexistente no es error", writeFirst: false, wantOK: false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			s, err := NewDiskStore(t.TempDir())
			if err != nil {
				t.Fatalf("preparación falló: %v", err)
			}

			if tc.writeFirst {
				if _, err := s.Write("blk-1", strings.NewReader("x")); err != nil {
					t.Fatalf("preparación falló: %v", err)
				}
			}

			ok, err := s.Delete("blk-1")
			if err != nil {
				t.Fatalf("Delete() error inesperado: %v", err)
			}

			if ok != tc.wantOK {
				t.Fatalf("Delete() ok = %v, se esperaba %v", ok, tc.wantOK)
			}
		})
	}
}

func TestVerify(t *testing.T) {
	s, err := NewDiskStore(t.TempDir())
	if err != nil {
		t.Fatalf("preparación falló: %v", err)
	}

	data := []byte("datos para verificar")
	if _, err := s.Write("blk-1", bytes.NewReader(data)); err != nil {
		t.Fatalf("Write() error inesperado: %v", err)
	}

	report, exists, err := s.Verify("blk-1")
	if err != nil {
		t.Fatalf("Verify() error inesperado: %v", err)
	}

	if !exists {
		t.Fatalf("Verify() exists = false, se esperaba true")
	}

	if report.SHA256 != hashOf(data) {
		t.Fatalf("Verify() sha256 = %q, se esperaba %q", report.SHA256, hashOf(data))
	}

	if _, exists, _ := s.Verify("no-existe"); exists {
		t.Fatalf("Verify() exists = true para un bloque inexistente")
	}
}

func TestList(t *testing.T) {
	s, err := NewDiskStore(t.TempDir())
	if err != nil {
		t.Fatalf("preparación falló: %v", err)
	}

	for _, id := range []string{"blk-1", "blk-2", "blk-3"} {
		if _, err := s.Write(id, strings.NewReader(id)); err != nil {
			t.Fatalf("preparación falló: %v", err)
		}
	}

	reports, err := s.List()
	if err != nil {
		t.Fatalf("List() error inesperado: %v", err)
	}

	if len(reports) != 3 {
		t.Fatalf("List() devolvió %d bloques, se esperaban 3", len(reports))
	}
}

func TestWriteConcurrent(t *testing.T) {
	s, err := NewDiskStore(t.TempDir())
	if err != nil {
		t.Fatalf("preparación falló: %v", err)
	}

	const n = 50

	var wg sync.WaitGroup
	errs := make(chan error, n)

	for i := 0; i < n; i++ {
		wg.Add(1)

		go func(i int) {
			defer wg.Done()

			id := fmt.Sprintf("blk-%d", i)
			if _, err := s.Write(id, strings.NewReader(id)); err != nil {
				errs <- err
			}
		}(i)
	}

	wg.Wait()
	close(errs)

	for err := range errs {
		t.Fatalf("Write() concurrente falló: %v", err)
	}

	reports, err := s.List()
	if err != nil {
		t.Fatalf("List() error inesperado: %v", err)
	}

	if len(reports) != n {
		t.Fatalf("List() devolvió %d bloques, se esperaban %d", len(reports), n)
	}
}
