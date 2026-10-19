package rest

import (
	"log"
	"net/http"

	"dfsha/internal/server/datanode/blockstore"
	"dfsha/internal/server/metanode/service"
	"dfsha/internal/server/metanode/store"
)

type dataNodeReportRequest struct {
	DataNodeID string                   `json:"datanode_id"`
	Address    string                   `json:"address"`
	FreeBytes  int64                    `json:"free_bytes"`
	BlockCount int                      `json:"block_count"`
	Blocks     []blockstore.BlockReport `json:"blocks"`
}

func (srv *Server) handleRegisterDataNode(
	w http.ResponseWriter,
	r *http.Request,
) {
	var req dataNodeReportRequest
	if !decodeJSONLimit(w, r, &req, 32<<20) {
		return
	}

	if req.FreeBytes < 0 || req.BlockCount < 0 {
		writeError(w, http.StatusBadRequest, "invalid datanode status")
		return
	}

	if err := service.RegisterDataNode(
		srv.store,
		req.DataNodeID,
		req.Address,
	); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	if err := service.ReceiveHeartbeat(
		srv.store,
		req.DataNodeID,
		req.FreeBytes,
	); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if req.Blocks != nil {
		if err := reconcileDataNodeInventory(srv.store, req.DataNodeID, req.Blocks); err != nil {
			writeServiceError(w, err)
			return
		}
	}

	log.Printf("MetaNode: datanode registered id=%s address=%s free_bytes=%d reported_blocks=%d inventory_received=%t", req.DataNodeID, req.Address, req.FreeBytes, req.BlockCount, req.Blocks != nil)
	writeJSON(w, http.StatusCreated, map[string]string{
		"status": "registered",
	})
}

type heartbeatResponse struct {
	Status       string   `json:"status"`
	DeleteBlocks []string `json:"delete_blocks"` // bloques que este DataNode debe borrar
}

func (srv *Server) handleDataNodeHeartbeat(
	w http.ResponseWriter,
	r *http.Request,
) {
	var req dataNodeReportRequest
	if !decodeJSONLimit(w, r, &req, 32<<20) {
		return
	}

	if req.DataNodeID == "" || req.FreeBytes < 0 || req.BlockCount < 0 {
		writeError(w, http.StatusBadRequest, "invalid heartbeat")
		return
	}

	if err := service.ReceiveHeartbeat(
		srv.store,
		req.DataNodeID,
		req.FreeBytes,
	); err != nil {
		writeError(w, http.StatusNotFound, err.Error())
		return
	}

	if req.Blocks != nil {
		if err := reconcileDataNodeInventory(srv.store, req.DataNodeID, req.Blocks); err != nil {
			writeServiceError(w, err)
			return
		}
	}

	toDelete, err := pendingDeletionsForReport(srv.store, req.DataNodeID, req.Blocks)
	if err != nil {
		writeServiceError(w, err)
		return
	}
	log.Printf("MetaNode: heartbeat received datanode=%s free_bytes=%d reported_blocks=%d inventory_received=%t delete_commands=%d", req.DataNodeID, req.FreeBytes, req.BlockCount, req.Blocks != nil, len(toDelete))

	writeJSON(w, http.StatusOK, heartbeatResponse{
		Status:       "alive",
		DeleteBlocks: toDelete,
	})
}

type reportBlockRequest struct {
	BlockID    string `json:"block_id"`
	DataNodeID string `json:"datanode_id"`
	SHA256     string `json:"sha256"`
}

// handleReportBlock registra la confirmación de un bloque.
func (srv *Server) handleReportBlock(w http.ResponseWriter, r *http.Request) {
	var req reportBlockRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	if req.BlockID == "" || req.DataNodeID == "" {
		writeError(w, http.StatusBadRequest, "block yielded an invalid report")
		return
	}
	if err := service.ReportBlockStored(srv.store, req.BlockID, req.DataNodeID, req.SHA256); err != nil {
		log.Printf("MetaNode: block report failed block_id=%s datanode=%s error=%v", req.BlockID, req.DataNodeID, err)
		writeServiceError(w, err)
		return
	}
	log.Printf("MetaNode: block stored confirmation received block_id=%s datanode=%s sha256=%s", req.BlockID, req.DataNodeID, req.SHA256)
	w.WriteHeader(http.StatusNoContent)
}

// reconcileDataNodeInventory treats the file catalog as authoritative. Local
// blocks that are no longer referenced, have no location entry, belong to a
// different node, or fail the recorded hash are queued for deletion. It also
// repairs crash windows where a file update committed before its old block
// deletion queue was created.
func reconcileDataNodeInventory(s store.Store, dataNodeID string, inventory []blockstore.BlockReport) error {
	seen := make(map[string]blockstore.BlockReport, len(inventory))
	for _, item := range inventory {
		if item.BlockID == "" {
			continue
		}
		seen[item.BlockID] = item

		location, locationErr := s.GetBlockLocation(item.BlockID)
		referenced := s.IsBlockReferenced(item.BlockID)
		stale := !referenced
		hashMismatch := false
		if locationErr == nil {
			if !containsDataNode(location.DataNodes, dataNodeID) {
				stale = true
			}
			expectedHash := location.SHA256
			if expectedHash == "" {
				expectedHash = location.Confirmations[dataNodeID]
			}
			if expectedHash != "" && item.SHA256 != "" && expectedHash != item.SHA256 {
				stale = true
				hashMismatch = true
				log.Printf("MetaNode: block inventory hash mismatch datanode=%s block_id=%s expected_sha256=%s reported_sha256=%s", dataNodeID, item.BlockID, expectedHash, item.SHA256)
			}
		} else if referenced {
			// A referenced block with missing location metadata is an inconsistent
			// catalog. Keep the physical data rather than risk deleting the only copy.
			log.Printf("MetaNode: block has file references but no location metadata; leaving it untouched block_id=%s datanode=%s", item.BlockID, dataNodeID)
			continue
		}

		if stale {
			if hashMismatch && referenced && containsDataNode(location.DataNodes, dataNodeID) {
				location.DataNodes = removeDataNode(location.DataNodes, dataNodeID)
				delete(location.Confirmations, dataNodeID)
				if err := s.SaveBlockLocation(location); err != nil {
					return err
				}
				log.Printf("MetaNode: removed corrupt replica from block catalog datanode=%s block_id=%s", dataNodeID, item.BlockID)
			}
			if err := s.EnqueueBlockDeletion(dataNodeID, item.BlockID); err != nil {
				return err
			}
			log.Printf("MetaNode: queued stale block for deletion datanode=%s block_id=%s referenced=%t location_found=%t", dataNodeID, item.BlockID, referenced, locationErr == nil)
		}
	}

	// Clean block-location entries left behind if the process stopped between
	// publishing a replacement file and queuing deletion of the previous blocks.
	locations, err := s.ListBlockLocations()
	if err != nil {
		return err
	}
	for _, location := range locations {
		if s.IsBlockReferenced(location.BlockID) {
			continue
		}
		queuedAll := true
		for _, nodeID := range location.DataNodes {
			if err := s.EnqueueBlockDeletion(nodeID, location.BlockID); err != nil {
				queuedAll = false
				return err
			}
		}
		if queuedAll {
			if err := s.DeleteBlockLocation(location.BlockID); err != nil {
				return err
			}
			log.Printf("MetaNode: removed orphaned block metadata block_id=%s datanodes=%v", location.BlockID, location.DataNodes)
		}
	}

	// If the inventory is complete and a previously confirmed replica has gone
	// missing from disk, stop advertising this node for that block. Do not touch
	// newly allocated blocks that have not yet been uploaded/confirmed.
	locations, err = s.ListBlockLocations()
	if err != nil {
		return err
	}
	for _, location := range locations {
		if !s.IsBlockReferenced(location.BlockID) || !containsDataNode(location.DataNodes, dataNodeID) {
			continue
		}
		if _, present := seen[location.BlockID]; present {
			continue
		}
		if _, wasConfirmed := location.Confirmations[dataNodeID]; !wasConfirmed {
			continue
		}

		location.DataNodes = removeDataNode(location.DataNodes, dataNodeID)
		delete(location.Confirmations, dataNodeID)
		if err := s.SaveBlockLocation(location); err != nil {
			return err
		}
		log.Printf("MetaNode: removed missing replica from block catalog datanode=%s block_id=%s remaining_replicas=%d", dataNodeID, location.BlockID, len(location.DataNodes))
	}
	return nil
}

func pendingDeletionsForReport(s store.Store, dataNodeID string, inventory []blockstore.BlockReport) ([]string, error) {
	pending, err := s.ListBlockDeletions(dataNodeID)
	if err != nil {
		return nil, err
	}
	if inventory == nil {
		// Older or temporarily unhealthy DataNodes may not be able to provide a
		// reliable inventory. Send commands but keep them queued until confirmed.
		return pending, nil
	}

	present := make(map[string]struct{}, len(inventory))
	for _, item := range inventory {
		present[item.BlockID] = struct{}{}
	}

	toDelete := make([]string, 0, len(pending))
	acknowledged := make([]string, 0)
	for _, blockID := range pending {
		// An old deletion request must not delete a block that is referenced
		// again by current metadata and assigned to this DataNode.
		if s.IsBlockReferenced(blockID) {
			location, locationErr := s.GetBlockLocation(blockID)
			if locationErr != nil || containsDataNode(location.DataNodes, dataNodeID) {
				acknowledged = append(acknowledged, blockID)
				continue
			}
		}
		if inventory == nil {
			toDelete = append(toDelete, blockID)
			continue
		}
		if _, exists := present[blockID]; exists {
			toDelete = append(toDelete, blockID)
		} else {
			acknowledged = append(acknowledged, blockID)
		}
	}
	if err := s.AckBlockDeletions(dataNodeID, acknowledged); err != nil {
		return nil, err
	}
	return toDelete, nil
}

func containsDataNode(nodeIDs []string, wanted string) bool {
	for _, id := range nodeIDs {
		if id == wanted {
			return true
		}
	}
	return false
}

func removeDataNode(nodeIDs []string, remove string) []string {
	out := make([]string, 0, len(nodeIDs))
	for _, id := range nodeIDs {
		if id != remove {
			out = append(out, id)
		}
	}
	return out
}
