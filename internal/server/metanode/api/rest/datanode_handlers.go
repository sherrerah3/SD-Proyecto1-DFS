package rest

import (
	"net/http"

	"dfsha/internal/server/metanode/service"
)

type dataNodeReportRequest struct {
	DataNodeID string `json:"datanode_id"`
	Address    string `json:"address"`
	FreeBytes  int64  `json:"free_bytes"`
	BlockCount int    `json:"block_count"`
}

func (srv *Server) handleRegisterDataNode(
	w http.ResponseWriter,
	r *http.Request,
) {
	var req dataNodeReportRequest
	if !decodeJSON(w, r, &req) {
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
	if !decodeJSON(w, r, &req) {
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

	// Borrados pendientes de este DataNode.
	toDelete, err := srv.store.DequeueBlockDeletions(req.DataNodeID)
	if err != nil {
		toDelete = nil
	}

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
		writeServiceError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
