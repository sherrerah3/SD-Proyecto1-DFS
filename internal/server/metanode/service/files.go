package service

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"path"
	"sort"
	"strings"
	"sync/atomic"

	"github.com/google/uuid"

	"dfsha/internal/server/metanode/store"
)

const (
	DefaultBlockSizeBytes = 1 * 1024 * 1024
	ReplicationFactor     = 3
	MaxUserQuotaBytes     = 2 * 1024 * 1024 * 1024
	WriteQuorum           = 2
)

const BlockSizeBytes = DefaultBlockSizeBytes

var blockSizeBytes int64 = DefaultBlockSizeBytes

func SetBlockSize(size int64) {
	if size > 0 {
		blockSizeBytes = size
	}
}

func BlockSize() int64 { return blockSizeBytes }

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

var rrIndex atomic.Uint64

type BlockPlacement struct {
	BlockID   string
	DataNodes []string
}

// InitPut inicia una subida nueva. No sobrescribe un archivo terminado.
func InitPut(
	s store.Store,
	username, filePath string,
	sizeBytes int64,
) ([]BlockPlacement, error) {
	if sizeBytes <= 0 {
		return nil, ErrInvalidSize
	}

	if existing, err := s.GetFile(username, filePath); err == nil {
		if existing.SHA256 != "" {
			return nil, ErrAlreadyExists
		}
		if err := DeleteFile(s, username, filePath); err != nil {
			return nil, err
		}
	}

	return initPutAt(s, username, filePath, filePath, sizeBytes, sizeBytes)
}

// InitPutForUpdate inicia la subida de una nueva versión sin eliminar
// la versión que ya existe en la ruta visible.
func InitPutForUpdate(
	s store.Store,
	username, filePath string,
	sizeBytes int64,
) ([]BlockPlacement, error) {
	if sizeBytes <= 0 {
		return nil, ErrInvalidSize
	}

	filePath = path.Clean(filePath)

	existing, err := s.GetFile(username, filePath)
	if err != nil || existing.SHA256 == "" {
		return nil, ErrNotFound
	}

	stagePath := updateStagePath(username, filePath)

	if _, err := s.GetFile(username, stagePath); err == nil {
		if err := discardStagedUpdate(
			s,
			username,
			filePath,
			stagePath,
		); err != nil {
			return nil, fmt.Errorf(
				"no se pudo limpiar una actualización anterior: %w",
				err,
			)
		}
	}

	// La cuota refleja el tamaño de la nueva versión.
	quotaDelta := sizeBytes - existing.SizeBytes

	return initPutAt(
		s,
		username,
		filePath,
		stagePath,
		sizeBytes,
		quotaDelta,
	)
}

// initPutAt centraliza la preparación de la subida.
// logicalPath es la ruta visible; metadataPath puede ser temporal.
func initPutAt(
	s store.Store,
	username, logicalPath, metadataPath string,
	sizeBytes, quotaDelta int64,
) ([]BlockPlacement, error) {
	parent := path.Dir(logicalPath)
	if parent != "/" && !s.DirectoryExists(username, parent) {
		return nil, ErrParentMissing
	}

	numBlocks := int((sizeBytes + blockSizeBytes - 1) / blockSizeBytes)

	aliveNodes, err := s.ListAliveDataNodes()
	if err != nil {
		return nil, fmt.Errorf("error al listar datanodes: %w", err)
	}

	if len(aliveNodes) < ReplicationFactor {
		return nil, fmt.Errorf(
			"%w: hay %d pero se necesitan al menos %d",
			ErrNoDataNodes,
			len(aliveNodes),
			ReplicationFactor,
		)
	}

	sort.Slice(aliveNodes, func(i, j int) bool {
		return aliveNodes[i].ID < aliveNodes[j].ID
	})

	if err := s.ReserveQuota(
		username,
		quotaDelta,
		MaxUserQuotaBytes,
	); err != nil {
		if errors.Is(err, store.ErrQuotaExceeded) {
			return nil, ErrQuotaExceeded
		}
		return nil, fmt.Errorf("error al reservar cuota: %w", err)
	}

	releaseQuota := func() {
		_ = s.UpdateQuotaUsed(username, -quotaDelta)
	}

	placements := make([]BlockPlacement, 0, numBlocks)
	blockIDs := make([]string, 0, numBlocks)
	blockNodeIDs := make([][]string, 0, numBlocks)

	for i := 0; i < numBlocks; i++ {
		blockID := uuid.NewString()
		blockIDs = append(blockIDs, blockID)

		start := rrIndex.Add(ReplicationFactor) - ReplicationFactor

		nodeIDs := make([]string, 0, ReplicationFactor)
		addrs := make([]string, 0, ReplicationFactor)

		for r := uint64(0); r < ReplicationFactor; r++ {
			n := aliveNodes[(start+r)%uint64(len(aliveNodes))]
			nodeIDs = append(nodeIDs, n.ID)
			addrs = append(addrs, n.Address)
		}

		blockNodeIDs = append(blockNodeIDs, nodeIDs)
		placements = append(placements, BlockPlacement{
			BlockID:   blockID,
			DataNodes: addrs,
		})
	}

	if err := s.CreateFile(store.FileMetadata{
		Name:      path.Base(logicalPath),
		Path:      metadataPath,
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
			DataNodes: blockNodeIDs[i],
		}); err != nil {
			_ = s.DeleteFile(username, metadataPath)

			for _, id := range blockIDs {
				_ = s.DeleteBlockLocation(id)
			}

			releaseQuota()

			return nil, fmt.Errorf(
				"error al guardar bloque %s: %w",
				p.BlockID,
				err,
			)
		}
	}

	return placements, nil
}

// updateStagePath genera una ruta interna estable para la versión temporal.
func updateStagePath(username, filePath string) string {
	sum := sha256.Sum256(
		[]byte(username + "\x00" + path.Clean(filePath)),
	)

	return path.Join(
		path.Dir(filePath),
		".dfsha-stage-"+hex.EncodeToString(sum[:12]),
	)
}

func isUpdateStagePath(filePath string) bool {
	return strings.HasPrefix(path.Base(filePath), ".dfsha-stage-")
}

// discardStagedUpdate elimina una subida temporal incompleta y recupera
// la cuota de la versión que continúa publicada.
func discardStagedUpdate(
	s store.Store,
	username, targetPath, stagePath string,
) error {
	original, err := s.GetFile(username, targetPath)
	if err != nil {
		return ErrNotFound
	}

	if _, err := s.GetFile(username, stagePath); err != nil {
		return nil
	}

	if err := DeleteFile(s, username, stagePath); err != nil {
		return err
	}

	if err := s.UpdateQuotaUsed(username, original.SizeBytes); err != nil {
		return fmt.Errorf(
			"no se pudo restaurar la cuota del archivo original: %w",
			err,
		)
	}

	return nil
}

// AbortPutForUpdate descarta la nueva versión si no llegó a confirmarse.
func AbortPutForUpdate(
	s store.Store,
	username, filePath string,
) error {
	filePath = path.Clean(filePath)
	stagePath := updateStagePath(username, filePath)

	if _, err := s.GetFile(username, stagePath); err != nil {
		return nil
	}

	return discardStagedUpdate(
		s,
		username,
		filePath,
		stagePath,
	)
}

// CompletePutForUpdate publica la nueva versión una vez subida y confirmada.
// La sustitución de metadata es atómica en el store.
func CompletePutForUpdate(
	s store.Store,
	username, filePath, sha256Hash string,
) error {
	filePath = path.Clean(filePath)
	stagePath := updateStagePath(username, filePath)

	staged, err := s.GetFile(username, stagePath)
	if err != nil {
		return ErrNotFound
	}

	if staged.SHA256 == "" {
		if err := CompletePut(
			s,
			username,
			stagePath,
			sha256Hash,
		); err != nil {
			return err
		}

		staged, err = s.GetFile(username, stagePath)
		if err != nil {
			return ErrNotFound
		}
	} else if staged.SHA256 != sha256Hash {
		return ErrAlreadyCompleted
	}

	oldFile, err := s.ReplaceFileWithStaged(
		username,
		filePath,
		stagePath,
	)
	if err != nil {
		return fmt.Errorf(
			"no se pudo publicar la nueva versión: %w",
			err,
		)
	}

	// La nueva versión ya está publicada. Se liberan los bloques viejos.
	deleteFileBlocks(s, oldFile)

	return nil
}

// deleteFileBlocks encola el borrado de las réplicas de una versión antigua.
func deleteFileBlocks(s store.Store, file store.FileMetadata) {
	for _, blockID := range file.BlockIDs {
		block, err := s.GetBlockLocation(blockID)
		if err != nil {
			continue
		}

		for _, nodeID := range block.DataNodes {
			_ = s.EnqueueBlockDeletion(nodeID, blockID)
		}

		_ = s.DeleteBlockLocation(blockID)
	}
}

// CompletePut finaliza una subida y exige quórum para cada bloque.
func CompletePut(
	s store.Store,
	username, filePath, sha256Hash string,
) error {
	file, err := s.GetFile(username, filePath)
	if err != nil {
		return ErrNotFound
	}

	if file.SHA256 != "" {
		return ErrAlreadyCompleted
	}

	raw, err := hex.DecodeString(sha256Hash)
	if err != nil || len(raw) != 32 {
		return ErrInvalidHash
	}

	for _, blockID := range file.BlockIDs {
		block, err := s.GetBlockLocation(blockID)
		if err != nil {
			return ErrNotFound
		}

		if !hasQuorum(block) {
			return ErrQuorumNotMet
		}

		if block.SHA256 == "" {
			block.SHA256 = majorityHash(block)

			if err := s.SaveBlockLocation(block); err != nil {
				return fmt.Errorf(
					"error al fijar hash de bloque: %w",
					err,
				)
			}
		}
	}

	file.SHA256 = sha256Hash

	if err := s.UpdateFile(file); err != nil {
		return fmt.Errorf("error al actualizar archivo: %w", err)
	}

	return nil
}

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

func ReportBlockStored(
	s store.Store,
	blockID, dataNodeID, hash string,
) error {
	raw, err := hex.DecodeString(hash)
	if err != nil || len(raw) != 32 {
		return ErrInvalidHash
	}

	if err := s.ReportBlock(blockID, dataNodeID, hash); err != nil {
		return ErrNotFound
	}

	return nil
}

// ResolveGet devuelve los bloques y sus direcciones, con los nodos vivos primero.
func ResolveGet(
	s store.Store,
	username, filePath string,
) ([]BlockPlacement, string, error) {
	file, err := s.GetFile(username, filePath)
	if err != nil {
		return nil, "", ErrNotFound
	}

	if file.SHA256 == "" {
		return nil, "", ErrNotFound
	}

	addrByID, aliveByID := nodeIndex(s)
	blocks := make([]BlockPlacement, 0, len(file.BlockIDs))

	for _, blockID := range file.BlockIDs {
		block, err := s.GetBlockLocation(blockID)
		if err != nil {
			return nil, "", fmt.Errorf(
				"bloque %s no encontrado: %w",
				blockID,
				err,
			)
		}

		addrs := orderLiveFirst(
			block.DataNodes,
			addrByID,
			aliveByID,
		)

		blocks = append(blocks, BlockPlacement{
			BlockID:   blockID,
			DataNodes: addrs,
		})
	}

	return blocks, file.SHA256, nil
}

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

func orderLiveFirst(
	ids []string,
	addrByID map[string]string,
	aliveByID map[string]bool,
) []string {
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

// ListDir devuelve archivos completos que son hijos directos del directorio.
func ListDir(
	s store.Store,
	username, dirPath string,
) ([]store.FileMetadata, error) {
	dirPath = path.Clean(dirPath)

	all, err := s.ListFiles(username, dirPath)
	if err != nil {
		return nil, fmt.Errorf("error al listar archivos: %w", err)
	}

	owned := make([]store.FileMetadata, 0, len(all))

	for _, file := range all {
		if file.SHA256 != "" &&
			!isUpdateStagePath(file.Path) &&
			path.Dir(file.Path) == dirPath {
			owned = append(owned, file)
		}
	}

	return owned, nil
}

// DeleteFile elimina metadata y bloques, recuperando la cuota.
func DeleteFile(
	s store.Store,
	username, filePath string,
) error {
	file, err := s.GetFile(username, filePath)
	if err != nil {
		return ErrNotFound
	}

	// Si quedó una actualización temporal incompleta, se elimina antes.
	if !isUpdateStagePath(filePath) {
		stagePath := updateStagePath(username, filePath)

		if _, stageErr := s.GetFile(username, stagePath); stageErr == nil {
			if err := discardStagedUpdate(
				s,
				username,
				filePath,
				stagePath,
			); err != nil {
				return err
			}
		}
	}

	for _, blockID := range file.BlockIDs {
		block, err := s.GetBlockLocation(blockID)
		if err != nil {
			continue
		}

		for _, nodeID := range block.DataNodes {
			_ = s.EnqueueBlockDeletion(nodeID, blockID)
		}

		_ = s.DeleteBlockLocation(blockID)
	}

	if err := s.DeleteFile(username, filePath); err != nil {
		return fmt.Errorf("error al borrar archivo: %w", err)
	}

	if err := s.UpdateQuotaUsed(username, -file.SizeBytes); err != nil {
		return fmt.Errorf("error al actualizar cuota: %w", err)
	}

	return nil
}
