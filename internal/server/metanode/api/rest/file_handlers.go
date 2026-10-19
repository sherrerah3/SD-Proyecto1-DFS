package rest

import (
	"net/http"
	"path"
	"time"

	"dfsha/internal/server/metanode/service"
)

const (
	capabilityBaseTTL  = 30 * time.Second
	capabilityPerBlock = 1 * time.Second
)

func capabilityTTL(numBlocks int) time.Duration {
	return capabilityBaseTTL + time.Duration(numBlocks)*capabilityPerBlock
}

type blockDTO struct {
	BlockID   string   `json:"block_id"`
	DataNodes []string `json:"datanodes"`
}

type putInitRequest struct {
	Path       string `json:"path"`
	SizeBytes  int64  `json:"size_bytes"`
	LockID     string `json:"lock_id,omitempty"`
	FenceToken uint64 `json:"fence_token,omitempty"`
}

type putInitResponse struct {
	Blocks          []blockDTO `json:"blocks"`
	CapabilityToken string     `json:"capability_token"`
}

func (srv *Server) handlePutInit(
	w http.ResponseWriter,
	r *http.Request,
) {
	username := usernameFrom(r)

	var req putInitRequest
	if !decodeJSON(w, r, &req) {
		return
	}

	filePath, ok := cleanFilePath(req.Path)
	if !ok {
		writeError(
			w,
			http.StatusBadRequest,
			"ruta invalida: debe ser absoluta, ej. /docs/a.txt",
		)
		return
	}

	if err := srv.validateWriteLock(
		username,
		filePath,
		req.LockID,
		req.FenceToken,
	); err != nil {
		writeError(w, http.StatusConflict, err.Error())
		return
	}

	var placements []service.BlockPlacement
	var err error

	if req.LockID != "" {
		placements, err = service.InitPutForUpdate(
			srv.store,
			username,
			filePath,
			req.SizeBytes,
		)
	} else {
		placements, err = service.InitPut(
			srv.store,
			username,
			filePath,
			req.SizeBytes,
		)
	}

	if err != nil {
		writeServiceError(w, err)
		return
	}

	blocks := make([]blockDTO, 0, len(placements))
	ids := make([]string, 0, len(placements))

	for _, p := range placements {
		blocks = append(blocks, blockDTO{
			BlockID:   p.BlockID,
			DataNodes: p.DataNodes,
		})
		ids = append(ids, p.BlockID)
	}

	token, err := service.GenerateCapabilityToken(
		username,
		service.OpPut,
		ids,
		capabilityTTL(len(ids)),
	)
	if err != nil {
		writeServiceError(w, err)
		return
	}

	writeJSON(w, http.StatusOK, putInitResponse{
		Blocks:          blocks,
		CapabilityToken: token,
	})
}

type putCompleteRequest struct {
	Path       string `json:"path"`
	SHA256     string `json:"sha256"`
	LockID     string `json:"lock_id,omitempty"`
	FenceToken uint64 `json:"fence_token,omitempty"`
}

func (srv *Server) handlePutComplete(
	w http.ResponseWriter,
	r *http.Request,
) {
	var req putCompleteRequest
	if !decodeJSON(w, r, &req) {
		return
	}

	filePath, ok := cleanFilePath(req.Path)
	if !ok {
		writeError(w, http.StatusBadRequest, "ruta invalida")
		return
	}

	username := usernameFrom(r)

	if err := srv.validateWriteLock(
		username,
		filePath,
		req.LockID,
		req.FenceToken,
	); err != nil {
		writeError(w, http.StatusConflict, err.Error())
		return
	}

	var err error

	if req.LockID != "" {
		err = service.CompletePutForUpdate(
			srv.store,
			username,
			filePath,
			req.SHA256,
		)
	} else {
		err = service.CompletePut(
			srv.store,
			username,
			filePath,
			req.SHA256,
		)
	}

	if err != nil {
		writeServiceError(w, err)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

type getFileResponse struct {
	Blocks          []blockDTO `json:"blocks"`
	SHA256          string     `json:"sha256"`
	CapabilityToken string     `json:"capability_token"`
}

func (srv *Server) handleGetFile(
	w http.ResponseWriter,
	r *http.Request,
) {
	username := usernameFrom(r)

	filePath, ok := cleanFilePath("/" + r.PathValue("path"))
	if !ok {
		writeError(w, http.StatusBadRequest, "ruta invalida")
		return
	}

	blocks, sha, err := service.ResolveGet(
		srv.store,
		username,
		filePath,
	)
	if err != nil {
		writeServiceError(w, err)
		return
	}

	dtos := make([]blockDTO, 0, len(blocks))
	ids := make([]string, 0, len(blocks))

	for _, b := range blocks {
		dtos = append(dtos, blockDTO{
			BlockID:   b.BlockID,
			DataNodes: b.DataNodes,
		})
		ids = append(ids, b.BlockID)
	}

	token, err := service.GenerateCapabilityToken(
		username,
		service.OpGet,
		ids,
		capabilityTTL(len(ids)),
	)
	if err != nil {
		writeServiceError(w, err)
		return
	}

	writeJSON(w, http.StatusOK, getFileResponse{
		Blocks:          dtos,
		SHA256:          sha,
		CapabilityToken: token,
	})
}

type fileEntry struct {
	Name      string `json:"name"`
	Path      string `json:"path"`
	SizeBytes int64  `json:"size_bytes"`
	Owner     string `json:"owner"`
	Kind      string `json:"kind"`
}

func (srv *Server) handleListFiles(
	w http.ResponseWriter,
	r *http.Request,
) {
	dir, ok := cleanDirPath(r.URL.Query().Get("dir"))
	if !ok {
		writeError(w, http.StatusBadRequest, "directorio invalido")
		return
	}

	username := usernameFrom(r)

	files, err := service.ListDir(srv.store, username, dir)
	if err != nil {
		writeServiceError(w, err)
		return
	}

	directories, err := srv.store.ListDirectories(username, dir)
	if err != nil {
		writeError(
			w,
			http.StatusInternalServerError,
			"no se pudieron listar los directorios",
		)
		return
	}

	entries := make([]fileEntry, 0, len(files)+len(directories))

	for _, f := range files {
		entries = append(entries, fileEntry{
			Name:      f.Name,
			Path:      f.Path,
			SizeBytes: f.SizeBytes,
			Owner:     f.Owner,
			Kind:      "file",
		})
	}

	for _, name := range directories {
		dirPath := path.Join(dir, name)

		entries = append(entries, fileEntry{
			Name:  name,
			Path:  dirPath,
			Owner: username,
			Kind:  "directory",
		})
	}

	writeJSON(w, http.StatusOK, entries)
}

func (srv *Server) handleDeleteFile(
	w http.ResponseWriter,
	r *http.Request,
) {
	filePath, ok := cleanFilePath("/" + r.PathValue("path"))
	if !ok {
		writeError(w, http.StatusBadRequest, "ruta invalida")
		return
	}

	if err := srv.validateWriteLock(
		usernameFrom(r),
		filePath,
		"",
		0,
	); err != nil {
		writeError(w, http.StatusConflict, err.Error())
		return
	}

	if err := service.DeleteFile(
		srv.store,
		usernameFrom(r),
		filePath,
	); err != nil {
		writeServiceError(w, err)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}
