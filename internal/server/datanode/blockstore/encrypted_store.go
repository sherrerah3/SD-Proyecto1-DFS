package blockstore

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"

	"dfsha/internal/server/datanode/crypto"
)

// Aserción en tiempo de compilación: *EncryptedBlockStore debe cumplir BlockStore.
var _ BlockStore = (*EncryptedBlockStore)(nil)

// EncryptedBlockStore envuelve un BlockStore y cifra el contenido en reposo.
//
// El hash y el tamaño que reporta son siempre del contenido en claro, para
// mantener la consistencia con el SHA256 que registra el Metanode.
type EncryptedBlockStore struct {
	inner  BlockStore
	cipher crypto.Cipher
}

// NewEncryptedBlockStore envuelve inner con cifrado en reposo.
func NewEncryptedBlockStore(inner BlockStore, c crypto.Cipher) *EncryptedBlockStore {
	return &EncryptedBlockStore{inner: inner, cipher: c}
}

func (s *EncryptedBlockStore) Write(blockID string, r io.Reader) (BlockInfo, error) {
	// Se lee el bloque completo (1 MB por diseño) para hashear el claro y
	// cifrarlo con AES-GCM, que necesita el mensaje entero para el tag.
	plaintext, err := io.ReadAll(r)
	if err != nil {
		return BlockInfo{}, fmt.Errorf("falló la lectura del bloque %s: %w", blockID, err)
	}

	sum := sha256.Sum256(plaintext)

	ciphertext, err := s.cipher.Encrypt(plaintext)
	if err != nil {
		return BlockInfo{}, fmt.Errorf("falló el cifrado del bloque %s: %w", blockID, err)
	}

	if _, err := s.inner.Write(blockID, bytes.NewReader(ciphertext)); err != nil {
		return BlockInfo{}, err
	}

	return BlockInfo{
		BlockID:   blockID,
		SHA256:    hex.EncodeToString(sum[:]),
		SizeBytes: int64(len(plaintext)),
	}, nil
}

func (s *EncryptedBlockStore) Read(blockID string) (io.ReadCloser, BlockInfo, error) {
	rc, _, err := s.inner.Read(blockID)
	if err != nil {
		return nil, BlockInfo{}, err
	}

	defer rc.Close()

	ciphertext, err := io.ReadAll(rc)
	if err != nil {
		return nil, BlockInfo{}, fmt.Errorf("falló la lectura del bloque %s: %w", blockID, err)
	}

	plaintext, err := s.cipher.Decrypt(ciphertext)
	if err != nil {
		return nil, BlockInfo{}, fmt.Errorf("falló el descifrado del bloque %s: %w", blockID, err)
	}

	info := BlockInfo{BlockID: blockID, SizeBytes: int64(len(plaintext))}

	return io.NopCloser(bytes.NewReader(plaintext)), info, nil
}

func (s *EncryptedBlockStore) Delete(blockID string) (bool, error) {
	return s.inner.Delete(blockID)
}

func (s *EncryptedBlockStore) Verify(blockID string) (BlockReport, bool, error) {
	// Se descifra para hashear el contenido en claro, de modo que el reporte
	// sea comparable con el hash que guarda el Metanode.
	rc, _, err := s.Read(blockID)
	if err != nil {
		// El inner distingue "no encontrado" devolviendo exists=false abajo.
		if _, exists, verr := s.inner.Verify(blockID); verr == nil && !exists {
			return BlockReport{}, false, nil
		}

		return BlockReport{}, false, err
	}

	defer rc.Close()

	plaintext, err := io.ReadAll(rc)
	if err != nil {
		return BlockReport{}, false, fmt.Errorf("falló al verificar el bloque %s: %w", blockID, err)
	}

	sum := sha256.Sum256(plaintext)

	return BlockReport{BlockID: blockID, SHA256: hex.EncodeToString(sum[:])}, true, nil
}

func (s *EncryptedBlockStore) List() ([]BlockReport, error) {
	// Se parte de los IDs del inner y se recalcula cada hash sobre el claro.
	inner, err := s.inner.List()
	if err != nil {
		return nil, err
	}

	result := make([]BlockReport, 0, len(inner))

	for _, b := range inner {
		report, exists, err := s.Verify(b.BlockID)
		if err != nil {
			return nil, err
		}

		if !exists {
			continue
		}

		result = append(result, report)
	}

	return result, nil
}
