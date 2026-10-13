package service

import (
	"encoding/hex"
	"errors"
	"fmt"
	"path"
	"sort"
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
	ErrQuotaExceeded    = errors.New("cuota de almacenamiento excedida")
	ErrNotFound         = errors.New("archivo no encontrado")
	ErrNoDataNodes      = errors.New("no hay datanodes disponibles")
	ErrInvalidSize      = errors.New("el tamaño del archivo debe ser mayor a 0")
	ErrAlreadyExists    = errors.New("ya existe un archivo en esa ruta")
	ErrAlreadyCompleted = errors.New("la subida de este archivo ya fue completada")
	ErrInvalidHash      = errors.New("hash SHA256 inválido")
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
	if sizeBytes <= 0 {
		return nil, ErrInvalidSize
	}

	// Solo se permite reemplazar una subida abandonada (sin hash) del mismo usuario.
	if existing, err := s.GetFile(filePath); err == nil {
		if existing.Owner != username || existing.SHA256 != "" {
			return nil, ErrAlreadyExists
		}
		if err := DeleteFile(s, username, filePath); err != nil {
			return nil, err
		}
	}

	// Se lee el usuario DESPUES del posible borrado, para ver la cuota actualizada.
	user, err := s.GetUser(username)
	if err != nil {
		return nil, fmt.Errorf("usuario no encontrado: %w", err)
	}
	if user.QuotaUsedBytes+sizeBytes > MaxUserQuotaBytes {
		return nil, ErrQuotaExceeded
	}

	// Division entera hacia arriba (evita decimales de float64).
	numBlocks := int((sizeBytes + BlockSizeBytes - 1) / BlockSizeBytes)

	aliveNodes, err := s.ListAliveDataNodes()
	if err != nil {
		return nil, fmt.Errorf("error al listar datanodes: %w", err)
	}
	if len(aliveNodes) < ReplicationFactor {
		return nil, fmt.Errorf("%w: hay %d pero se necesitan al menos %d",
			ErrNoDataNodes, len(aliveNodes), ReplicationFactor)
	}
	// Un map de Go no tiene orden fijo; sin ordenar, el Round Robin seria aleatorio.
	sort.Slice(aliveNodes, func(i, j int) bool { return aliveNodes[i].ID < aliveNodes[j].ID })

	// Primero se arma todo en memoria.
	placements := make([]BlockPlacement, 0, numBlocks)
	blockIDs := make([]string, 0, numBlocks)
	for i := 0; i < numBlocks; i++ {
		blockID := uuid.NewString()
		blockIDs = append(blockIDs, blockID)

		// Reserva atomica de los indices del bloque: ninguna otra peticion se cuela en medio.
		start := rrIndex.Add(ReplicationFactor) - ReplicationFactor
		nodes := make([]string, 0, ReplicationFactor)
		for r := uint64(0); r < ReplicationFactor; r++ {
			nodes = append(nodes, aliveNodes[(start+r)%uint64(len(aliveNodes))].Address)
		}
		placements = append(placements, BlockPlacement{BlockID: blockID, DataNodes: nodes})
	}

	// CreateFile va primero: si la ruta esta ocupada, falla sin dejar bloques huerfanos.
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
	for _, p := range placements {
		if err := s.SaveBlockLocation(store.BlockMetadata{BlockID: p.BlockID, DataNodes: p.DataNodes}); err != nil {
			return nil, fmt.Errorf("error al guardar bloque %s: %w", p.BlockID, err)
		}
	}

	// La cuota se RESERVA aqui.
	if err := s.UpdateQuotaUsed(username, sizeBytes); err != nil {
		return nil, fmt.Errorf("error al reservar cuota: %w", err)
	}

	return placements, nil
}

// CompletePut cierra la subida: guarda el hash y descuenta la cuota del usuario
func CompletePut(s store.Store, username, filePath, sha256Hash string) error {
	file, err := s.GetFile(filePath)
	if err != nil || file.Owner != username {
		return ErrNotFound
	}
	if file.SHA256 != "" {
		return ErrAlreadyCompleted
	}
	// un SHA256 en hexadecimal mide siempre 64 caracteres (32 bytes)
	if raw, err := hex.DecodeString(sha256Hash); err != nil || len(raw) != 32 {
		return ErrInvalidHash
	}

	file.SHA256 = sha256Hash
	if err := s.UpdateFile(file); err != nil {
		return fmt.Errorf("error al actualizar archivo: %w", err)
	}
	return nil
}

// ResolveGet valida el dueño y devuelve los bloques del archivo y su hash
func ResolveGet(s store.Store, username, filePath string) ([]store.BlockMetadata, string, error) {
	file, err := s.GetFile(filePath)
	if err != nil || file.Owner != username {
		return nil, "", ErrNotFound
	}
	if file.SHA256 == "" {
		return nil, "", ErrNotFound // la subida aun no termina
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

// ListDir devuelve los archivos del usuario, ya subidos, que son hijos directos de dirPath
func ListDir(s store.Store, username, dirPath string) ([]store.FileMetadata, error) {
	// Se normaliza aqui para no depender de que el caller mande la ruta limpia.
	dirPath = path.Clean(dirPath)

	all, err := s.ListFiles(dirPath)
	if err != nil {
		return nil, fmt.Errorf("error al listar archivos: %w", err)
	}

	owned := make([]store.FileMetadata, 0, len(all))
	for _, file := range all {
		// del usuario, subida terminada, y hijo directo de dirPath
		if file.Owner == username && file.SHA256 != "" && path.Dir(file.Path) == dirPath {
			owned = append(owned, file)
		}
	}

	return owned, nil
}

// DeleteFile valida el dueño, borra la metadata y libera la cuota
func DeleteFile(s store.Store, username, filePath string) error {
	// Un archivo ajeno se trata como inexistente, igual que en ResolveGet.
	file, err := s.GetFile(filePath)
	if err != nil || file.Owner != username {
		return ErrNotFound
	}

	if err := s.DeleteFile(filePath); err != nil {
		return fmt.Errorf("error al borrar archivo: %w", err)
	}

	// Devuelve la cuota que InitPut habia reservado para este archivo.
	if err := s.UpdateQuotaUsed(username, -file.SizeBytes); err != nil {
		return fmt.Errorf("error al actualizar cuota: %w", err)
	}

	return nil
}
