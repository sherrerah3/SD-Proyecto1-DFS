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
	ErrNotFound         = errors.New("file not found")
	ErrNoDataNodes      = errors.New("no datanodes available")
	ErrInvalidSize      = errors.New("file size must be greater than 0")
	ErrAlreadyExists    = errors.New("a file already exists at this path")
	ErrAlreadyCompleted = errors.New("this file upload has already been completed")
	ErrInvalidHash      = errors.New("invalid SHA256 hash")
	ErrParentMissing    = errors.New("parent directory does not exist")
	ErrQuorumNotMet     = errors.New("a block did not reach the acknowledgment quorum")
)

var rrIndex atomic.Uint64

type BlockPlacement struct {
	BlockID   string
	DataNodes []string
}

// InitPut starts a new upload. It does not overwrite a completed file.
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

// InitPutForUpdate starts uploading a new version without deleting
// the version that already exists at the visible path.
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
				"failed to clean up a previous update: %w",
				err,
			)
		}
	}

	// The quota reflects the size of the new version.
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

// initPutAt centralizes upload preparation.
// logicalPath is the visible path; metadataPath may be temporary.
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
		return nil, fmt.Errorf("error listing datanodes: %w", err)
	}

	if len(aliveNodes) < ReplicationFactor {
		return nil, fmt.Errorf(
			"%w: found %d but at least %d are required",
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
		return nil, fmt.Errorf("error reserving quota: %w", err)
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

		return nil, fmt.Errorf("error registering file: %w", err)
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
				"error saving block %s: %w",
				p.BlockID,
				err,
			)
		}
	}

	return placements, nil
}

// updateStagePath generates a stable internal path for the temporary version.
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

// discardStagedUpdate removes an incomplete temporary upload and restores
// the quota for the version that remains published.
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
			"failed to restore the original file quota: %w",
			err,
		)
	}

	return nil
}

// AbortPutForUpdate discards the new version if it was not confirmed.
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

// CompletePutForUpdate publishes the new version once it has been uploaded and confirmed.
// The metadata replacement is atomic in the store.
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
			"failed to publish the new version: %w",
			err,
		)
	}

	// The new version is now published. Queue deletion of the old replicas.
	if err := deleteFileBlocks(s, oldFile); err != nil {
		return fmt.Errorf("new file version was published but old-block cleanup failed: %w", err)
	}

	return nil
}

// deleteFileBlocks durably queues deletion of replicas belonging to an old version.
func deleteFileBlocks(s store.Store, file store.FileMetadata) error {
	for _, blockID := range file.BlockIDs {
		// A block may still be referenced by another file or by the newly
		// published version. Never queue deletion for a block still in use.
		if s.IsBlockReferenced(blockID) {
			continue
		}
		block, err := s.GetBlockLocation(blockID)
		if err != nil {
			continue
		}

		for _, nodeID := range block.DataNodes {
			if err := s.EnqueueBlockDeletion(nodeID, blockID); err != nil {
				return fmt.Errorf("failed to queue deletion of block %s on datanode %s: %w", blockID, nodeID, err)
			}
		}

		if err := s.DeleteBlockLocation(blockID); err != nil {
			return fmt.Errorf("failed to remove location metadata for block %s: %w", blockID, err)
		}
	}
	return nil
}

// CompletePut finalizes an upload and requires a quorum for every block.
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
					"error setting block hash: %w",
					err,
				)
			}
		}
	}

	file.SHA256 = sha256Hash

	if err := s.UpdateFile(file); err != nil {
		return fmt.Errorf("error updating file: %w", err)
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

// ResolveGet returns the blocks and their addresses, with live nodes first.
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
				"block %s not found: %w",
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

// ListDir returns completed files that are direct children of the directory.
func ListDir(
	s store.Store,
	username, dirPath string,
) ([]store.FileMetadata, error) {
	dirPath = path.Clean(dirPath)

	all, err := s.ListFiles(username, dirPath)
	if err != nil {
		return nil, fmt.Errorf("error listing files: %w", err)
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

// DeleteFile removes metadata and blocks, restoring the quota.
func DeleteFile(
	s store.Store,
	username, filePath string,
) error {
	file, err := s.GetFile(username, filePath)
	if err != nil {
		return ErrNotFound
	}

	// If an incomplete temporary update remains, remove it first.
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

	// Remove the file reference first. If cleanup is interrupted, the
	// reconciliation pass can find orphaned block locations and queue them.
	if err := s.DeleteFile(username, filePath); err != nil {
		return fmt.Errorf("failed to delete file metadata: %w", err)
	}

	if err := deleteFileBlocks(s, file); err != nil {
		return fmt.Errorf("file was deleted but block cleanup failed: %w", err)
	}

	if err := s.UpdateQuotaUsed(username, -file.SizeBytes); err != nil {
		return fmt.Errorf("error updating quota: %w", err)
	}

	return nil
}
