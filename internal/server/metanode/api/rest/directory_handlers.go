package rest

import (
	"errors"
	"net/http"

	"dfsha/internal/server/metanode/store"
)

type createDirectoryRequest struct {
	Path string `json:"path"`
}

func (srv *Server) handleCreateDirectory(w http.ResponseWriter, r *http.Request) {
	var req createDirectoryRequest
	if !decodeJSON(w, r, &req) {
		return
	}

	dirPath, ok := cleanDirPath(req.Path)
	if !ok || dirPath == "/" {
		writeError(w, http.StatusBadRequest, "ruta de directorio inválida")
		return
	}

	err := srv.store.CreateDirectory(usernameFrom(r), dirPath)
	switch {
	case errors.Is(err, store.ErrDirectoryExists):
		writeError(w, http.StatusConflict, err.Error())
		return
	case errors.Is(err, store.ErrDirectoryNotFound):
		writeError(w, http.StatusNotFound, "el directorio padre no existe")
		return
	case err != nil:
		writeServiceError(w, err)
		return
	}

	w.WriteHeader(http.StatusCreated)
}

func (srv *Server) handleDirectoryExists(w http.ResponseWriter, r *http.Request) {
	dirPath, ok := cleanDirPath(r.URL.Query().Get("path"))
	if !ok {
		writeError(w, http.StatusBadRequest, "ruta de directorio inválida")
		return
	}

	if !srv.store.DirectoryExists(usernameFrom(r), dirPath) {
		writeError(w, http.StatusNotFound, "directorio no encontrado")
		return
	}

	writeJSON(w, http.StatusOK, map[string]bool{"exists": true})
}

func (srv *Server) handleDeleteDirectory(w http.ResponseWriter, r *http.Request) {
	dirPath, ok := cleanDirPath(r.URL.Query().Get("path"))
	if !ok || dirPath == "/" {
		writeError(w, http.StatusBadRequest, "invalid directory path")
		return
	}

	err := srv.store.DeleteDirectory(usernameFrom(r), dirPath)
	switch {
	case errors.Is(err, store.ErrDirectoryNotFound):
		writeError(w, http.StatusNotFound, err.Error())
		return
	case errors.Is(err, store.ErrDirectoryNotEmpty):
		writeError(w, http.StatusConflict, err.Error())
		return
	case err != nil:
		writeServiceError(w, err)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}
