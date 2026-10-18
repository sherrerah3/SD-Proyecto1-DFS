package rest

import (
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"path"
	"strings"

	"dfsha/internal/server/metanode/service"
	"dfsha/internal/server/metanode/store"
)

type errorResponse struct {
	Error string `json:"error"`
}

// writeJSON escribe una respuesta con el codigo HTTP y el cuerpo en JSON.
func writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if body != nil {
		if err := json.NewEncoder(w).Encode(body); err != nil {
			log.Printf("error escribiendo respuesta: %v", err)
		}
	}
}

func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, errorResponse{Error: msg})
}

// decodeJSON lee el cuerpo; devuelve false y ya responde el error si el JSON esta mal.
func decodeJSON(w http.ResponseWriter, r *http.Request, dst any) bool {
	// Tope de 1 MB: aqui solo viaja metadata, nunca bytes de archivos.
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields() // rechaza campos que no esperamos
	if err := dec.Decode(dst); err != nil {
		writeError(w, http.StatusBadRequest, "JSON invalido")
		return false
	}
	return true
}

// statusFor traduce un error de service/ al codigo HTTP que le corresponde.
func statusFor(err error) int {
	switch {
	case errors.Is(err, service.ErrInvalidUsername),
		errors.Is(err, service.ErrInvalidPassword),
		errors.Is(err, service.ErrInvalidSize),
		errors.Is(err, service.ErrInvalidHash):
		return http.StatusBadRequest // 400
	case errors.Is(err, service.ErrInvalidCredentials),
		errors.Is(err, service.ErrInvalidToken):
		return http.StatusUnauthorized // 401
	case errors.Is(err, service.ErrQuotaExceeded),
		errors.Is(err, service.ErrTooManyUsers):
		return http.StatusForbidden // 403
	case errors.Is(err, service.ErrNotFound),
		errors.Is(err, service.ErrParentMissing):
		return http.StatusNotFound // 404
	case errors.Is(err, service.ErrUserAlreadyExists),
		errors.Is(err, service.ErrAlreadyExists),
		errors.Is(err, service.ErrAlreadyCompleted),
		errors.Is(err, service.ErrQuorumNotMet),
		errors.Is(err, store.ErrDirectoryNotEmpty):
		return http.StatusConflict // 409
	case errors.Is(err, service.ErrNoDataNodes):
		return http.StatusServiceUnavailable // 503
	case errors.Is(err, store.ErrNotLeader):
		return http.StatusServiceUnavailable // 503
	default:
		return http.StatusInternalServerError // 500
	}
}

// writeServiceError responde segun el error; los 500 se loguean pero no se muestran al cliente.
func writeServiceError(w http.ResponseWriter, err error) {
	status := statusFor(err)
	if status == http.StatusInternalServerError {
		log.Printf("error interno: %v", err)
		writeError(w, status, "error interno del servidor")
		return
	}
	writeError(w, status, err.Error())
}

// cleanFilePath exige ruta absoluta de archivo y normaliza "/docs/../x" o "//".
func cleanFilePath(p string) (string, bool) {
	if p == "" || len(p) > 512 || !strings.HasPrefix(p, "/") || strings.ContainsRune(p, 0) {
		return "", false
	}
	clean := path.Clean(p)
	if clean == "/" {
		return "", false // la raiz no es un archivo
	}
	return clean, true
}

// cleanDirPath igual que la anterior, pero permite la raiz "/" y asume "/" si viene vacia.
func cleanDirPath(p string) (string, bool) {
	if p == "" {
		return "/", true
	}
	if len(p) > 512 || !strings.HasPrefix(p, "/") || strings.ContainsRune(p, 0) {
		return "", false
	}
	return path.Clean(p), true
}
