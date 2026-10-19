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
		{name: "writes a new block", blockID: "blk-1", data: []byte("hello world"), wantErr: false},
		{name: "writes an empty block", blockID: "blk-empty", data: []byte{}, wantErr: false},
		{name: "rechaza un empty block ID", blockID: "", data: []byte("x"), wantErr: true},
		{name: "rechaza un block ID con separador", blockID: "a/b", data: []byte("x"), wantErr: true},
		{name: "rechaza un block ID con traversal", blockID: "..", data: []byte("x"), wantErr: true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			s, err := NewDiskStore(t.TempDir())
			if err != nil {
				t.Fatalf("setup failed: %v", err)
			}

			info, err := s.Write(tc.blockID, bytes.NewReader(tc.data))
			if (err != nil) != tc.wantErr {
				t.Fatalf("Write() error = %v, wantErr = %v", err, tc.wantErr)
			}

			if tc.wantErr {
				return
			}

			if info.SizeBytes != int64(len(tc.data)) {
				t.Fatalf("Write() size = %d, expected %d", info.SizeBytes, len(tc.data))
			}

			if info.SHA256 != hashOf(tc.data) {
				t.Fatalf("Write() sha256 = %q, expected %q", info.SHA256, hashOf(tc.data))
			}
		})
	}
}

func TestWriteReplacesExistingBlock(t *testing.T) {
	s, err := NewDiskStore(t.TempDir())
	if err != nil {
		t.Fatalf("setup failed: %v", err)
	}

	if _, err := s.Write("reused-block", strings.NewReader("old content")); err != nil {
		t.Fatalf("initial Write() failed: %v", err)
	}
	if _, err := s.Write("reused-block", strings.NewReader("new content")); err != nil {
		t.Fatalf("replacing an existing block failed: %v", err)
	}

	rc, _, err := s.Read("reused-block")
	if err != nil {
		t.Fatalf("Read() after replacing the block failed: %v", err)
	}
	defer rc.Close()
	got, err := io.ReadAll(rc)
	if err != nil {
		t.Fatalf("ReadAll() failed: %v", err)
	}
	if string(got) != "new content" {
		t.Fatalf("block content = %q, expected %q", string(got), "new content")
	}
}

func TestReadRoundTrip(t *testing.T) {
	s, err := NewDiskStore(t.TempDir())
	if err != nil {
		t.Fatalf("setup failed: %v", err)
	}

	data := []byte("test block content")
	if _, err := s.Write("blk-1", bytes.NewReader(data)); err != nil {
		t.Fatalf("Write() error unexpected: %v", err)
	}

	rc, info, err := s.Read("blk-1")
	if err != nil {
		t.Fatalf("Read() error unexpected: %v", err)
	}

	defer rc.Close()

	got, err := io.ReadAll(rc)
	if err != nil {
		t.Fatalf("ReadAll() error unexpected: %v", err)
	}

	if !bytes.Equal(got, data) {
		t.Fatalf("Read() returned %q, expected %q", got, data)
	}

	if info.SizeBytes != int64(len(data)) {
		t.Fatalf("Read() size = %d, expected %d", info.SizeBytes, len(data))
	}
}

func TestReadNotFound(t *testing.T) {
	s, err := NewDiskStore(t.TempDir())
	if err != nil {
		t.Fatalf("setup failed: %v", err)
	}

	if _, _, err := s.Read("no-existe"); err == nil {
		t.Fatalf("Read() should fail for a missing block")
	}
}

func TestDelete(t *testing.T) {
	tests := []struct {
		name       string
		writeFirst bool
		wantOK     bool
	}{
		{name: "deletes an existing block", writeFirst: true, wantOK: true},
		{name: "deleting a missing block is not an error", writeFirst: false, wantOK: false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			s, err := NewDiskStore(t.TempDir())
			if err != nil {
				t.Fatalf("setup failed: %v", err)
			}

			if tc.writeFirst {
				if _, err := s.Write("blk-1", strings.NewReader("x")); err != nil {
					t.Fatalf("setup failed: %v", err)
				}
			}

			ok, err := s.Delete("blk-1")
			if err != nil {
				t.Fatalf("Delete() error unexpected: %v", err)
			}

			if ok != tc.wantOK {
				t.Fatalf("Delete() ok = %v, expected %v", ok, tc.wantOK)
			}
		})
	}
}

func TestVerify(t *testing.T) {
	s, err := NewDiskStore(t.TempDir())
	if err != nil {
		t.Fatalf("setup failed: %v", err)
	}

	data := []byte("data to verify")
	if _, err := s.Write("blk-1", bytes.NewReader(data)); err != nil {
		t.Fatalf("Write() error unexpected: %v", err)
	}

	report, exists, err := s.Verify("blk-1")
	if err != nil {
		t.Fatalf("Verify() error unexpected: %v", err)
	}

	if !exists {
		t.Fatalf("Verify() exists = false, expected true")
	}

	if report.SHA256 != hashOf(data) {
		t.Fatalf("Verify() sha256 = %q, expected %q", report.SHA256, hashOf(data))
	}

	if _, exists, _ := s.Verify("no-existe"); exists {
		t.Fatalf("Verify() exists = true for a missing block")
	}
}

func TestList(t *testing.T) {
	s, err := NewDiskStore(t.TempDir())
	if err != nil {
		t.Fatalf("setup failed: %v", err)
	}

	for _, id := range []string{"blk-1", "blk-2", "blk-3"} {
		if _, err := s.Write(id, strings.NewReader(id)); err != nil {
			t.Fatalf("setup failed: %v", err)
		}
	}

	reports, err := s.List()
	if err != nil {
		t.Fatalf("List() error unexpected: %v", err)
	}

	if len(reports) != 3 {
		t.Fatalf("List() returned %d blocks, expected 3", len(reports))
	}
}

func TestWriteConcurrent(t *testing.T) {
	s, err := NewDiskStore(t.TempDir())
	if err != nil {
		t.Fatalf("setup failed: %v", err)
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
		t.Fatalf("concurrent Write() failed: %v", err)
	}

	reports, err := s.List()
	if err != nil {
		t.Fatalf("List() error unexpected: %v", err)
	}

	if len(reports) != n {
		t.Fatalf("List() returned %d blocks, expected %d", len(reports), n)
	}
}
