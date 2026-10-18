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
	ErrStaleVersion     = errors.New("el archivo cambió desde que se descargó")
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

	placements, blockNodeIDs, err := allocateBlocks(s, numBlocksFor(sizeBytes))
	if err != nil {
		return nil, err
	}
	blockIDs := blockIDsOf(placements)

	// Reserva atómica de cuota antes de tocar la metadata.
	if err := s.ReserveQuota(username, sizeBytes, MaxUserQuotaBytes); err != nil {
		if errors.Is(err, store.ErrQuotaExceeded) {
			return nil, ErrQuotaExceeded
		}
		return nil, fmt.Errorf("error al reservar cuota: %w", err)
	}

	// Si algo falla despues, se devuelve la cuota.
	releaseQuota := func() { _ = s.UpdateQuotaUsed(username, -sizeBytes) }

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
	if err := saveBlocks(s, placements, blockNodeIDs); err != nil {
		releaseQuota()
		return nil, err
	}

	return placements, nil
}

// numBlocksFor calcula cuántos bloques ocupa un archivo (división hacia arriba).
func numBlocksFor(sizeBytes int64) int {
	return int((sizeBytes + blockSizeBytes - 1) / blockSizeBytes)
}

func blockIDsOf(placements []BlockPlacement) []string {
	ids := make([]string, 0, len(placements))
	for _, p := range placements {
		ids = append(ids, p.BlockID)
	}
	return ids
}

// allocateBlocks reparte bloques en Round Robin y devuelve sus direcciones y IDs de nodo.
func allocateBlocks(s store.Store, numBlocks int) ([]BlockPlacement, [][]string, error) {
	aliveNodes, err := s.ListAliveDataNodes()
	if err != nil {
		return nil, nil, fmt.Errorf("error al listar datanodes: %w", err)
	}
	if len(aliveNodes) < ReplicationFactor {
		return nil, nil, fmt.Errorf("%w: hay %d pero se necesitan al menos %d",
			ErrNoDataNodes, len(aliveNodes), ReplicationFactor)
	}
	// Un map de Go no tiene orden fijo; sin ordenar, el Round Robin sería aleatorio.
	sort.Slice(aliveNodes, func(i, j int) bool { return aliveNodes[i].ID < aliveNodes[j].ID })

	placements := make([]BlockPlacement, 0, numBlocks)
	nodeIDsPerBlock := make([][]string, 0, numBlocks)
	for i := 0; i < numBlocks; i++ {
		// Reserva atómica de los índices del bloque: ninguna otra petición se cuela.
		start := rrIndex.Add(ReplicationFactor) - ReplicationFactor
		nodeIDs := make([]string, 0, ReplicationFactor)
		addrs := make([]string, 0, ReplicationFactor)
		for r := uint64(0); r < ReplicationFactor; r++ {
			n := aliveNodes[(start+r)%uint64(len(aliveNodes))]
			nodeIDs = append(nodeIDs, n.ID)
			addrs = append(addrs, n.Address)
		}
		nodeIDsPerBlock = append(nodeIDsPerBlock, nodeIDs)
		placements = append(placements, BlockPlacement{BlockID: uuid.NewString(), DataNodes: addrs})
	}
	return placements, nodeIDsPerBlock, nil
}

// discardBlocks encola el borrado de los bloques y olvida su ubicación.
func discardBlocks(s store.Store, blockIDs []string) {
	for _, blockID := range blockIDs {
		block, err := s.GetBlockLocation(blockID)
		if err != nil {
			continue // el bloque ya no está registrado
		}
		for _, nodeID := range block.DataNodes {
			_ = s.EnqueueBlockDeletion(nodeID, blockID)
		}
		_ = s.DeleteBlockLocation(blockID)
	}
}

// InitReplace reemplaza un archivo completo; la versión actual sigue legible hasta CompletePut.
func InitReplace(s store.Store, username, filePath string, sizeBytes int64, baseSHA string) ([]BlockPlacement, error) {
	if sizeBytes <= 0 {
		return nil, ErrInvalidSize
	}

	cur, err := s.GetFile(username, filePath)
	if err != nil || cur.SHA256 == "" {
		return nil, ErrNotFound
	}
	if cur.SHA256 != baseSHA {
		return nil, ErrStaleVersion
	}

	placements, nodeIDs, err := allocateBlocks(s, numBlocksFor(sizeBytes))
	if err != nil {
		return nil, err
	}
	blockIDs := blockIDsOf(placements)

	reserved, err := reserveGrowth(s, username, sizeBytes, cur.SizeBytes)
	if err != nil {
		return nil, err
	}

	undo := func() {
		if reserved > 0 {
			_ = s.UpdateQuotaUsed(username, -reserved)
		}
		for _, id := range blockIDs {
			_ = s.DeleteBlockLocation(id)
		}
	}

	if err := saveBlocks(s, placements, nodeIDs); err != nil {
		undo()
		return nil, err
	}

	prev, err := s.ModifyFile(username, filePath, func(f *store.FileMetadata) error {
		if f.SHA256 == "" {
			return ErrNotFound
		}
		if f.SHA256 != baseSHA {
			return ErrStaleVersion
		}
		f.Pending = &store.PendingVersion{SizeBytes: sizeBytes, BlockIDs: blockIDs, ReservedBytes: reserved}
		return nil
	})
	if err != nil {
		undo()
		if errors.Is(err, store.ErrFileNotFound) {
			return nil, ErrNotFound
		}
		return nil, err
	}

	discardPending(s, username, prev.Pending)

	return placements, nil
}

// reserveGrowth reserva solo lo que crece el archivo: el tamaño viejo ya está contado.
func reserveGrowth(s store.Store, username string, newSize, oldSize int64) (int64, error) {
	growth := newSize - oldSize
	if growth <= 0 {
		return 0, nil
	}

	if err := s.ReserveQuota(username, growth, MaxUserQuotaBytes); err != nil {
		if errors.Is(err, store.ErrQuotaExceeded) {
			return 0, ErrQuotaExceeded
		}
		return 0, fmt.Errorf("error al reservar cuota: %w", err)
	}

	return growth, nil
}

// saveBlocks registra la ubicación de cada bloque (se guardan IDs de nodo, no direcciones).
func saveBlocks(s store.Store, placements []BlockPlacement, nodeIDs [][]string) error {
	for i, p := range placements {
		block := store.BlockMetadata{BlockID: p.BlockID, DataNodes: nodeIDs[i]}
		if err := s.SaveBlockLocation(block); err != nil {
			return fmt.Errorf("error al guardar bloque %s: %w", p.BlockID, err)
		}
	}

	return nil
}

// discardPending descarta un reemplazo abandonado: libera su cuota y sus bloques.
func discardPending(s store.Store, username string, pending *store.PendingVersion) {
	if pending == nil {
		return
	}
	if pending.ReservedBytes > 0 {
		_ = s.UpdateQuotaUsed(username, -pending.ReservedBytes)
	}
	discardBlocks(s, pending.BlockIDs)
}

// CompletePut cierra la subida: exige el quórum por bloque y guarda el hash.
// Si hay una versión pendiente, la confirma en lugar de la actual.
func CompletePut(s store.Store, username, filePath, sha256Hash string) error {
	file, err := s.GetFile(username, filePath)
	if err != nil {
		return ErrNotFound
	}
	if file.Pending == nil && file.SHA256 != "" {
		return ErrAlreadyCompleted
	}
	// un SHA256 en hexadecimal mide siempre 64 caracteres (32 bytes)
	if raw, err := hex.DecodeString(sha256Hash); err != nil || len(raw) != 32 {
		return ErrInvalidHash
	}

	if file.Pending != nil {
		return completeReplace(s, username, filePath, file.Pending, sha256Hash)
	}

	if err := confirmBlocks(s, file.BlockIDs); err != nil {
		return err
	}

	file.SHA256 = sha256Hash
	if err := s.UpdateFile(file); err != nil {
		return fmt.Errorf("error al actualizar archivo: %w", err)
	}
	return nil
}

// completeReplace cambia la versión actual por la pendiente de forma atómica.
func completeReplace(s store.Store, username, filePath string, pending *store.PendingVersion, sha256Hash string) error {
	if err := confirmBlocks(s, pending.BlockIDs); err != nil {
		return err
	}

	prev, err := s.ModifyFile(username, filePath, func(f *store.FileMetadata) error {
		// Otro reemplazo pudo haber sustituido la versión pendiente.
		if f.Pending == nil || len(f.Pending.BlockIDs) == 0 || f.Pending.BlockIDs[0] != pending.BlockIDs[0] {
			return ErrStaleVersion
		}
		f.SizeBytes = f.Pending.SizeBytes
		f.BlockIDs = f.Pending.BlockIDs
		f.SHA256 = sha256Hash
		f.Pending = nil
		return nil
	})
	if err != nil {
		if errors.Is(err, store.ErrFileNotFound) {
			return ErrNotFound
		}
		return err
	}

	// La cuota queda en el tamaño nuevo: ya se reservó lo que crecía.
	if adjust := pending.SizeBytes - prev.SizeBytes - pending.ReservedBytes; adjust != 0 {
		if err := s.UpdateQuotaUsed(username, adjust); err != nil {
			return fmt.Errorf("error al actualizar cuota: %w", err)
		}
	}
	discardBlocks(s, prev.BlockIDs)
	return nil
}

// confirmBlocks exige el quórum de cada bloque y fija su hash.
func confirmBlocks(s store.Store, blockIDs []string) error {
	// Cada bloque necesita WriteQuorum confirmaciones con el mismo hash.
	for _, blockID := range blockIDs {
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

// ResolveGet devuelve los bloques, el hash y el tamaño, con las réplicas vivas primero
func ResolveGet(s store.Store, username, filePath string) ([]BlockPlacement, string, int64, error) {
	file, err := s.GetFile(username, filePath)
	if err != nil {
		return nil, "", 0, ErrNotFound
	}
	if file.SHA256 == "" {
		return nil, "", 0, ErrNotFound // la subida aún no termina
	}

	addrByID, aliveByID := nodeIndex(s)

	blocks := make([]BlockPlacement, 0, len(file.BlockIDs))
	for _, blockID := range file.BlockIDs {
		block, err := s.GetBlockLocation(blockID)
		if err != nil {
			return nil, "", 0, fmt.Errorf("bloque %s no encontrado: %w", blockID, err)
		}
		addrs := orderLiveFirst(block.DataNodes, addrByID, aliveByID)
		blocks = append(blocks, BlockPlacement{BlockID: blockID, DataNodes: addrs})
	}

	return blocks, file.SHA256, file.SizeBytes, nil
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

	// Se encolan los borrados por DataNode (incluida la versión pendiente).
	discardBlocks(s, file.BlockIDs)
	released := file.SizeBytes
	if file.Pending != nil {
		discardBlocks(s, file.Pending.BlockIDs)
		released += file.Pending.ReservedBytes
	}

	if err := s.DeleteFile(username, filePath); err != nil {
		return fmt.Errorf("error al borrar archivo: %w", err)
	}

	// Devuelve la cuota que se había reservado para este archivo.
	if err := s.UpdateQuotaUsed(username, -released); err != nil {
		return fmt.Errorf("error al actualizar cuota: %w", err)
	}

	return nil
}
