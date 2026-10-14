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
		writeError(w, http.StatusBadRequest, "estado del datanode inválido")
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

func (srv *Server) handleDataNodeHeartbeat(
	w http.ResponseWriter,
	r *http.Request,
) {
	var req dataNodeReportRequest
	if !decodeJSON(w, r, &req) {
		return
	}

	if req.DataNodeID == "" || req.FreeBytes < 0 || req.BlockCount < 0 {
		writeError(w, http.StatusBadRequest, "heartbeat inválido")
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

	writeJSON(w, http.StatusOK, map[string]string{
		"status": "alive",
	})
}
