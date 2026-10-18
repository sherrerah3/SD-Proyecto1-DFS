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
	DefaultBlockSizeBytes = 1 * 1024 * 1024        // 1MB por bloque (valor por defecto)
	ReplicationFactor     = 3                      // 3 copias por bloque
	MaxUserQuotaBytes     = 2 * 1024 * 1024 * 1024 // 2GB por usuario
	WriteQuorum           = 2                      // confirmaciones mínimas por bloque
)

// BlockSizeBytes es alias del tamaño por defecto
const BlockSizeBytes = DefaultBlockSizeBytes

// blockSizeBytes es el tamaño efectivo (configurable)
var blockSizeBytes int64 = DefaultBlockSizeBytes

// SetBlockSize ajusta el tamaño de bloque al arrancar
func SetBlockSize(size int64) {
	if size > 0 {
		blockSizeBytes = size
	}
}

// BlockSize devuelve el tamaño de bloque efectivo
func BlockSize() int64 { return blockSizeBytes }

// Errores tipados para que la API REST los convierta a codigos HTTP.
var (
	ErrQuotaExceeded    = store.ErrQuotaExceeded
	ErrNotFound         = errors.New("archivo no encontrado")
	ErrNoDataNodes      = errors.New("no hay datanodes disponibles")
	ErrInvalidSize      = errors.New("el tamaño del archivo debe ser mayor a 0")
	ErrAlreadyExists    = errors.New("ya existe un archivo en esa ruta")
	ErrAlreadyCompleted = errors.New("la subida de este archivo ya fue completada")
	ErrInvalidHash      = errors.New("hash SHA256 inválido")
	ErrParentMissing    = errors.New("el directorio padre no existe")
	ErrQuorumNotMet     = errors.New("un bloque no alcanzó el quórum de confirmaciones")
)

// Indice global de Round Robin que avanza por cada replica asignada.
var rrIndex atomic.Uint64

// BlockPlacement describe donde va un bloque (DataNodes son direcciones host:puerto)
type BlockPlacement struct {
	BlockID   string
	DataNodes []string
}

// InitPut inicia la subida: valida cuota, reparte bloques y registra la metadata.
func InitPut(s store.Store, username, filePath string, sizeBytes int64) ([]BlockPlacement, error) {
	if sizeBytes <= 0 {
		return nil, ErrInvalidSize
	}

	// El directorio padre debe existir (salvo la raíz).
	parent := path.Dir(filePath)
	if parent != "/" && !s.DirectoryExists(username, parent) {
		return nil, ErrParentMissing
	}

	// Solo se permite reemplazar una subida abandonada (sin hash) del mismo usuario.
	if existing, err := s.GetFile(username, filePath); err == nil {
		if existing.SHA256 != "" {
			return nil, ErrAlreadyExists
		}
		if err := DeleteFile(s, username, filePath); err != nil {
			return nil, err
		}
	}

	numBlocks := int((sizeBytes + blockSizeBytes - 1) / blockSizeBytes)

	aliveNodes, err := s.ListAliveDataNodes()
	if err != nil {
		return nil, fmt.Errorf("error al listar datanodes: %w", err)
	}
	if len(aliveNodes) < ReplicationFactor {
		return nil, fmt.Errorf("%w: hay %d pero se necesitan al menos %d",
			ErrNoDataNodes, len(aliveNodes), ReplicationFactor)
	}
	// Un map de Go no tiene orden fijo; sin ordenar, el Round Robin sería aleatorio.
	sort.Slice(aliveNodes, func(i, j int) bool { return aliveNodes[i].ID < aliveNodes[j].ID })

	// Reserva atómica de cuota antes de tocar la metadata.
	if err := s.ReserveQuota(username, sizeBytes, MaxUserQuotaBytes); err != nil {
		if errors.Is(err, store.ErrQuotaExceeded) {
			return nil, ErrQuotaExceeded
		}
		return nil, fmt.Errorf("error al reservar cuota: %w", err)
	}

	// Si algo falla despues, se devuelve la cuota.
	releaseQuota := func() { _ = s.UpdateQuotaUsed(username, -sizeBytes) }

	placements := make([]BlockPlacement, 0, numBlocks)
	blockIDs := make([]string, 0, numBlocks)
	blockNodeIDs := make([][]string, 0, numBlocks)
	for i := 0; i < numBlocks; i++ {
		blockID := uuid.NewString()
		blockIDs = append(blockIDs, blockID)

		// Reserva atómica de los índices del bloque: ninguna otra petición se cuela.
		start := rrIndex.Add(ReplicationFactor) - ReplicationFactor
		nodeIDs := make([]string, 0, ReplicationFactor)
		addrs := make([]string, 0, ReplicationFactor)
		for r := uint64(0); r < ReplicationFactor; r++ {
			n := aliveNodes[(start+r)%uint64(len(aliveNodes))]
			nodeIDs = append(nodeIDs, n.ID)
			addrs = append(addrs, n.Address)
		}
		blockNodeIDs = append(blockNodeIDs, nodeIDs)
		placements = append(placements, BlockPlacement{BlockID: blockID, DataNodes: addrs})
	}

	// CreateFile va primero: si la ruta está ocupada, falla sin dejar bloques huérfanos.
	if err := s.CreateFile(store.FileMetadata{
		Name:      path.Base(filePath),
		Path:      filePath,
		Owner:     username,
		SizeBytes: sizeBytes,
		SHA256:    "",
		BlockIDs:  blockIDs,
	}); err != nil {
		releaseQuota()
		if errors.Is(err, store.ErrFileAlreadyExists) {
			return nil, ErrAlreadyExists
		}
		return nil, fmt.Errorf("error al registrar archivo: %w", err)
	}
	for i, p := range placements {
		if err := s.SaveBlockLocation(store.BlockMetadata{
			BlockID:   p.BlockID,
			DataNodes: blockNodeIDs[i], // se guardan IDs, no direcciones
		}); err != nil {
			releaseQuota()
			return nil, fmt.Errorf("error al guardar bloque %s: %w", p.BlockID, err)
		}
	}

	return placements, nil
}

// CompletePut cierra la subida: exige el quórum por bloque y guarda el hash.
func CompletePut(s store.Store, username, filePath, sha256Hash string) error {
	file, err := s.GetFile(username, filePath)
	if err != nil {
		return ErrNotFound
	}
	if file.SHA256 != "" {
		return ErrAlreadyCompleted
	}
	// un SHA256 en hexadecimal mide siempre 64 caracteres (32 bytes)
	if raw, err := hex.DecodeString(sha256Hash); err != nil || len(raw) != 32 {
		return ErrInvalidHash
	}

	// Cada bloque necesita WriteQuorum confirmaciones con el mismo hash.
	for _, blockID := range file.BlockIDs {
		block, err := s.GetBlockLocation(blockID)
		if err != nil {
			return ErrNotFound
		}
		if !hasQuorum(block) {
			return ErrQuorumNotMet
		}
		// Fija el hash de la mayoría.
		if block.SHA256 == "" {
			block.SHA256 = majorityHash(block)
			if err := s.SaveBlockLocation(block); err != nil {
				return fmt.Errorf("error al fijar hash de bloque: %w", err)
			}
		}
	}

	file.SHA256 = sha256Hash
	if err := s.UpdateFile(file); err != nil {
		return fmt.Errorf("error al actualizar archivo: %w", err)
	}
	return nil
}

// hasQuorum indica si hay WriteQuorum confirmaciones con el mismo hash
func hasQuorum(b store.BlockMetadata) bool {
	counts := map[string]int{}
	for _, h := range b.Confirmations {
		counts[h]++
		if counts[h] >= WriteQuorum {
			return true
		}
	}
	return false
}

// majorityHash devuelve el hash más confirmado
func majorityHash(b store.BlockMetadata) string {
	counts := map[string]int{}
	best, bestN := "", 0
	for _, h := range b.Confirmations {
		counts[h]++
		if counts[h] > bestN {
			best, bestN = h, counts[h]
		}
	}
	return best
}

// ReportBlockStored registra la confirmación de un DataNode
func ReportBlockStored(s store.Store, blockID, dataNodeID, hash string) error {
	if raw, err := hex.DecodeString(hash); err != nil || len(raw) != 32 {
		return ErrInvalidHash
	}
	if err := s.ReportBlock(blockID, dataNodeID, hash); err != nil {
		return ErrNotFound
	}
	return nil
}

// ResolveGet devuelve los bloques y el hash, con las réplicas vivas primero
func ResolveGet(s store.Store, username, filePath string) ([]BlockPlacement, string, error) {
	file, err := s.GetFile(username, filePath)
	if err != nil {
		return nil, "", ErrNotFound
	}
	if file.SHA256 == "" {
		return nil, "", ErrNotFound // la subida aún no termina
	}

	addrByID, aliveByID := nodeIndex(s)

	blocks := make([]BlockPlacement, 0, len(file.BlockIDs))
	for _, blockID := range file.BlockIDs {
		block, err := s.GetBlockLocation(blockID)
		if err != nil {
			return nil, "", fmt.Errorf("bloque %s no encontrado: %w", blockID, err)
		}
		addrs := orderLiveFirst(block.DataNodes, addrByID, aliveByID)
		blocks = append(blocks, BlockPlacement{BlockID: blockID, DataNodes: addrs})
	}

	return blocks, file.SHA256, nil
}

// nodeIndex arma los mapas id->dirección e id->vivo
func nodeIndex(s store.Store) (map[string]string, map[string]bool) {
	addrByID := map[string]string{}
	aliveByID := map[string]bool{}
	nodes, err := s.ListDataNodes()
	if err != nil {
		return addrByID, aliveByID
	}
	for _, n := range nodes {
		addrByID[n.ID] = n.Address
		aliveByID[n.ID] = n.Alive
	}
	return addrByID, aliveByID
}

// orderLiveFirst traduce IDs a direcciones, vivos primero
func orderLiveFirst(ids []string, addrByID map[string]string, aliveByID map[string]bool) []string {
	live := make([]string, 0, len(ids))
	dead := make([]string, 0, len(ids))
	for _, id := range ids {
		addr, ok := addrByID[id]
		if !ok {
			continue
		}
		if aliveByID[id] {
			live = append(live, addr)
		} else {
			dead = append(dead, addr)
		}
	}
	return append(live, dead...)
}

// ListDir devuelve los archivos del usuario, ya subidos, que son hijos directos de dirPath.
func ListDir(s store.Store, username, dirPath string) ([]store.FileMetadata, error) {
	// Se normaliza aquí para no depender de que el caller mande la ruta limpia.
	dirPath = path.Clean(dirPath)

	all, err := s.ListFiles(username, dirPath)
	if err != nil {
		return nil, fmt.Errorf("error al listar archivos: %w", err)
	}

	owned := make([]store.FileMetadata, 0, len(all))
	for _, file := range all {
		// subida terminada e hijo directo de dirPath (el dueño ya lo filtró el store)
		if file.SHA256 != "" && path.Dir(file.Path) == dirPath {
			owned = append(owned, file)
		}
	}

	return owned, nil
}

// DeleteFile borra la metadata, libera la cuota y encola el borrado de bloques
func DeleteFile(s store.Store, username, filePath string) error {
	// Un archivo ajeno se trata como inexistente, igual que en ResolveGet.
	file, err := s.GetFile(username, filePath)
	if err != nil {
		return ErrNotFound
	}

	// Se encolan los borrados por DataNode.
	for _, blockID := range file.BlockIDs {
		block, err := s.GetBlockLocation(blockID)
		if err != nil {
			continue // el bloque ya no está registrado; nada que encolar
		}
		for _, nodeID := range block.DataNodes {
			_ = s.EnqueueBlockDeletion(nodeID, blockID)
		}
		_ = s.DeleteBlockLocation(blockID)
	}

	if err := s.DeleteFile(username, filePath); err != nil {
		return fmt.Errorf("error al borrar archivo: %w", err)
	}

	// Devuelve la cuota que InitPut había reservado para este archivo.
	if err := s.UpdateQuotaUsed(username, -file.SizeBytes); err != nil {
		return fmt.Errorf("error al actualizar cuota: %w", err)
	}

	return nil
}
