package blockstore

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

// Aserción en tiempo de compilación: *DiskStore debe cumplir BlockStore.
var _ BlockStore = (*DiskStore)(nil)

// Prefijo de los temporales de escritura. Se usa un prefijo propio (en vez de
// un sufijo del blockID) para distinguirlos sin ambigüedad al listar.
const tempPrefix = ".tmp-"

// DiskStore implementa BlockStore sobre un directorio local,
// protegido por un único sync.RWMutex.
type DiskStore struct {
	mu  sync.RWMutex
	dir string
}

// NewDiskStore crea el directorio de datos si no existe y devuelve el store.
func NewDiskStore(dir string) (*DiskStore, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, fmt.Errorf("no se pudo crear el directorio de bloques %s: %w", dir, err)
	}

	return &DiskStore{dir: dir}, nil
}

// blockPath valida el blockID y arma su ruta en disco.
func (s *DiskStore) blockPath(blockID string) (string, error) {
	if blockID == "" {
		return "", fmt.Errorf("block ID vacío")
	}

	if strings.ContainsAny(blockID, `/\`) || strings.Contains(blockID, "..") {
		return "", fmt.Errorf("block ID inválido: %s", blockID)
	}

	return filepath.Join(s.dir, blockID), nil
}

func (s *DiskStore) Write(blockID string, r io.Reader) (BlockInfo, error) {
	path, err := s.blockPath(blockID)
	if err != nil {
		return BlockInfo{}, err
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	// Se escribe primero a un temporal y luego se renombra, para que nunca
	// quede un bloque a medias si falla la escritura.
	tmp, err := os.CreateTemp(s.dir, tempPrefix+blockID)
	if err != nil {
		return BlockInfo{}, fmt.Errorf("no se pudo crear el temporal del bloque %s: %w", blockID, err)
	}

	tmpPath := tmp.Name()

	hasher := sha256.New()
	written, err := io.Copy(io.MultiWriter(tmp, hasher), r)
	if err != nil {
		tmp.Close()
		os.Remove(tmpPath)
		return BlockInfo{}, fmt.Errorf("falló la escritura del bloque %s: %w", blockID, err)
	}

	if err := tmp.Close(); err != nil {
		os.Remove(tmpPath)
		return BlockInfo{}, fmt.Errorf("falló al cerrar el temporal del bloque %s: %w", blockID, err)
	}

	if err := os.Rename(tmpPath, path); err != nil {
		os.Remove(tmpPath)
		return BlockInfo{}, fmt.Errorf("falló al confirmar el bloque %s: %w", blockID, err)
	}

	return BlockInfo{
		BlockID:   blockID,
		SHA256:    hex.EncodeToString(hasher.Sum(nil)),
		SizeBytes: written,
	}, nil
}

func (s *DiskStore) Read(blockID string) (io.ReadCloser, BlockInfo, error) {
	filePath, err := s.blockPath(blockID)
	if err != nil {
		return nil, BlockInfo{}, err
	}

	s.mu.RLock()
	defer s.mu.RUnlock()

	f, err := os.Open(filePath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, BlockInfo{}, fmt.Errorf("bloque no encontrado: %s", blockID)
		}
		return nil, BlockInfo{}, fmt.Errorf(
			"no se pudo abrir el bloque %s: %w",
			blockID,
			err,
		)
	}
	defer f.Close()

	data, err := io.ReadAll(f)
	if err != nil {
		return nil, BlockInfo{}, fmt.Errorf(
			"no se pudo leer el bloque %s: %w",
			blockID,
			err,
		)
	}

	sum := sha256.Sum256(data)

	info := BlockInfo{
		BlockID:   blockID,
		SHA256:    hex.EncodeToString(sum[:]),
		SizeBytes: int64(len(data)),
	}

	return io.NopCloser(bytes.NewReader(data)), info, nil
}

func (s *DiskStore) Delete(blockID string) (bool, error) {
	path, err := s.blockPath(blockID)
	if err != nil {
		return false, err
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	if err := os.Remove(path); err != nil {
		if os.IsNotExist(err) {
			return false, nil
		}

		return false, fmt.Errorf("falló al borrar el bloque %s: %w", blockID, err)
	}

	return true, nil
}

func (s *DiskStore) Verify(blockID string) (BlockReport, bool, error) {
	path, err := s.blockPath(blockID)
	if err != nil {
		return BlockReport{}, false, err
	}

	s.mu.RLock()
	defer s.mu.RUnlock()

	sum, err := hashFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return BlockReport{}, false, nil
		}

		return BlockReport{}, false, fmt.Errorf("falló al verificar el bloque %s: %w", blockID, err)
	}

	return BlockReport{BlockID: blockID, SHA256: sum}, true, nil
}

func (s *DiskStore) List() ([]BlockReport, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	entries, err := os.ReadDir(s.dir)
	if err != nil {
		return nil, fmt.Errorf("no se pudo listar el directorio de bloques: %w", err)
	}

	result := make([]BlockReport, 0)

	for _, e := range entries {
		// Se ignoran directorios y temporales a medio escribir.
		if e.IsDir() || strings.HasPrefix(e.Name(), tempPrefix) {
			continue
		}

		sum, err := hashFile(filepath.Join(s.dir, e.Name()))
		if err != nil {
			return nil, fmt.Errorf("falló al verificar el bloque %s: %w", e.Name(), err)
		}

		result = append(result, BlockReport{BlockID: e.Name(), SHA256: sum})
	}

	return result, nil
}

// hashFile calcula el SHA256 de un archivo leyéndolo en streaming.
func hashFile(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}

	defer f.Close()

	hasher := sha256.New()
	if _, err := io.Copy(hasher, f); err != nil {
		return "", err
	}

	return hex.EncodeToString(hasher.Sum(nil)), nil
}
