package service

import (
	"errors"
	"fmt"
	"math"
	"path"
	"sync/atomic"

	"github.com/google/uuid"

	"dfsha/internal/server/metanode/store"
)

// Parametros fijos del sistema
const (
	BlockSizeBytes    = 1 * 1024 * 1024        // 1MB por bloque
	ReplicationFactor = 3                      // 3 copias por bloque
	MaxUserQuotaBytes = 2 * 1024 * 1024 * 1024 // 2GB por usuario
)

// Errores tipados para que la API REST los convierta a codigos HTTP
var (
	ErrQuotaExceeded = errors.New("cuota de almacenamiento excedida")
	ErrForbidden     = errors.New("no tienes permiso sobre este archivo")
	ErrNotFound      = errors.New("archivo no encontrado")
	ErrNoDataNodes   = errors.New("no hay datanodes disponibles")
)

// Indice global de Round Robin que avanza por cada replica asignada
var rrIndex atomic.Uint64

// Describe donde va a vivir un bloque
type BlockPlacement struct {
	BlockID   string
	DataNodes []string // direcciones (host:puerto) de los DataNodes asignados
}

// InitPut inicia la subida: valida cuota, reparte bloques y registra la metadata
func InitPut(s store.Store, username, filePath string, sizeBytes int64) ([]BlockPlacement, error) {
	user, err := s.GetUser(username)
	if err != nil {
		return nil, fmt.Errorf("usuario no encontrado: %w", err)
	}

	// la cuota se valida antes de gastar trabajo en el reparto
	if user.QuotaUsedBytes+sizeBytes > MaxUserQuotaBytes {
		return nil, ErrQuotaExceeded
	}

	// division hacia arriba para no quedar cortos un bloque
	numBlocks := int(math.Ceil(float64(sizeBytes) / float64(BlockSizeBytes)))

	aliveNodes, err := s.ListAliveDataNodes()
	if err != nil {
		return nil, fmt.Errorf("error al listar datanodes: %w", err)
	}
	if len(aliveNodes) < ReplicationFactor {
		return nil, fmt.Errorf("%w: hay %d pero se necesitan al menos %d",
			ErrNoDataNodes, len(aliveNodes), ReplicationFactor)
	}

	placements := make([]BlockPlacement, 0, numBlocks)
	blockIDs := make([]string, 0, numBlocks)

	// por cada bloque, ID unico y ReplicationFactor datanodes por Round Robin
	for i := 0; i < numBlocks; i++ {
		blockID := uuid.NewString()
		blockIDs = append(blockIDs, blockID)

		nodes := make([]string, 0, ReplicationFactor)
		for r := 0; r < ReplicationFactor; r++ {
			idx := int(rrIndex.Add(1)-1) % len(aliveNodes)
			nodes = append(nodes, aliveNodes[idx].Address)
		}

		// anotar la ubicacion de cada bloque
		if err := s.SaveBlockLocation(store.BlockMetadata{
			BlockID:   blockID,
			DataNodes: nodes,
		}); err != nil {
			return nil, fmt.Errorf("error al guardar bloque %s: %w", blockID, err)
		}

		placements = append(placements, BlockPlacement{
			BlockID:   blockID,
			DataNodes: nodes,
		})
	}

	// registrar el archivo sin hash todavia (aun no termina de subir)
	if err := s.CreateFile(store.FileMetadata{
		Name:      path.Base(filePath),
		Path:      filePath,
		Owner:     username,
		SizeBytes: sizeBytes,
		SHA256:    "",
		BlockIDs:  blockIDs,
	}); err != nil {
		return nil, fmt.Errorf("error al registrar archivo: %w", err)
	}

	// devolver el plan; a partir de aqui el cliente habla con los DataNodes
	return placements, nil
}

// CompletePut cierra la subida: guarda el hash y descuenta la cuota del usuario
func CompletePut(s store.Store, username, filePath, sha256Hash string) error {
	file, err := s.GetFile(filePath)
	if err != nil {
		return ErrNotFound
	}

	if file.Owner != username {
		return ErrForbidden
	}

	// La interfaz Store no tiene update, asi que se borra y se recrea con el hash
	file.SHA256 = sha256Hash
	if err := s.DeleteFile(filePath); err != nil {
		return fmt.Errorf("error al actualizar archivo: %w", err)
	}
	if err := s.CreateFile(file); err != nil {
		return fmt.Errorf("error al actualizar archivo: %w", err)
	}

	// La cuota se cobra aqui, cuando la subida ya termino de verdad
	if err := s.UpdateQuotaUsed(username, file.SizeBytes); err != nil {
		return fmt.Errorf("error al actualizar cuota: %w", err)
	}

	return nil
}

// ResolveGet valida el dueño y devuelve los bloques del archivo y su hash
func ResolveGet(s store.Store, username, filePath string) ([]store.BlockMetadata, string, error) {
	file, err := s.GetFile(filePath)
	if err != nil {
		return nil, "", ErrNotFound
	}

	if file.Owner != username {
		return nil, "", ErrForbidden
	}

	blocks := make([]store.BlockMetadata, 0, len(file.BlockIDs))
	for _, blockID := range file.BlockIDs {
		block, err := s.GetBlockLocation(blockID)
		if err != nil {
			return nil, "", fmt.Errorf("bloque %s no encontrado: %w", blockID, err)
		}
		blocks = append(blocks, block)
	}

	return blocks, file.SHA256, nil
}

// ListDir devuelve los archivos del usuario que cuelgan de dirPath
func ListDir(s store.Store, username, dirPath string) ([]store.FileMetadata, error) {
	all, err := s.ListFiles(dirPath)
	if err != nil {
		return nil, fmt.Errorf("error al listar archivos: %w", err)
	}

	owned := make([]store.FileMetadata, 0, len(all))
	for _, file := range all {
		if file.Owner == username {
			owned = append(owned, file)
		}
	}

	return owned, nil
}

// DeleteFile valida el dueño, borra la metadata y libera la cuota
func DeleteFile(s store.Store, username, filePath string) error {
	file, err := s.GetFile(filePath)
	if err != nil {
		return ErrNotFound
	}

	if file.Owner != username {
		return ErrForbidden
	}

	if err := s.DeleteFile(filePath); err != nil {
		return fmt.Errorf("error al borrar archivo: %w", err)
	}

	// Lo contrario exacto de CompletePut: devolver la cuota usada
	if err := s.UpdateQuotaUsed(username, -file.SizeBytes); err != nil {
		return fmt.Errorf("error al actualizar cuota: %w", err)
	}

	return nil
}
