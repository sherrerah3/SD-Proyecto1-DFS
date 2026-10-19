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

func writeJSON(
	w http.ResponseWriter,
	status int,
	body any,
) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)

	if body != nil {
		if err := json.NewEncoder(w).Encode(body); err != nil {
			log.Printf("error writing response: %v", err)
		}
	}
}

func writeError(
	w http.ResponseWriter,
	status int,
	msg string,
) {
	writeJSON(w, status, errorResponse{Error: msg})
}

func decodeJSON(w http.ResponseWriter, r *http.Request, dst any) bool {
	return decodeJSONLimit(w, r, dst, 1<<20)
}

func decodeJSONLimit(w http.ResponseWriter, r *http.Request, dst any, maxBytes int64) bool {
	r.Body = http.MaxBytesReader(w, r.Body, maxBytes)

	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(dst); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON")
		return false
	}
	return true
}

func statusFor(err error) int {
	switch {
	case errors.Is(err, service.ErrInvalidUsername),
		errors.Is(err, service.ErrInvalidPassword),
		errors.Is(err, service.ErrInvalidSize),
		errors.Is(err, service.ErrInvalidHash):
		return http.StatusBadRequest

	case errors.Is(err, service.ErrInvalidCredentials),
		errors.Is(err, service.ErrInvalidToken):
		return http.StatusUnauthorized

	case errors.Is(err, service.ErrQuotaExceeded),
		errors.Is(err, service.ErrTooManyUsers):
		return http.StatusForbidden

	case errors.Is(err, service.ErrNotFound),
		errors.Is(err, service.ErrParentMissing):
		return http.StatusNotFound

	case errors.Is(err, service.ErrUserAlreadyExists),
		errors.Is(err, service.ErrAlreadyExists),
		errors.Is(err, service.ErrAlreadyCompleted),
		errors.Is(err, service.ErrQuorumNotMet),
		errors.Is(err, store.ErrDirectoryNotEmpty):
		return http.StatusConflict

	case errors.Is(err, service.ErrNoDataNodes):
		return http.StatusServiceUnavailable

	case errors.Is(err, store.ErrNotLeader):
		return http.StatusServiceUnavailable

	default:
		return http.StatusInternalServerError
	}
}

func writeServiceError(w http.ResponseWriter, err error) {
	status := statusFor(err)

	if status == http.StatusInternalServerError {
		log.Printf("internal error: %v", err)
		writeError(w, status, "internal server error")
		return
	}

	writeError(w, status, err.Error())
}

func cleanFilePath(p string) (string, bool) {
	if p == "" ||
		len(p) > 512 ||
		!strings.HasPrefix(p, "/") ||
		strings.ContainsRune(p, 0) {
		return "", false
	}

	clean := path.Clean(p)
	if clean == "/" {
		return "", false
	}

	// Staging paths are internal.
	for _, segment := range strings.Split(strings.Trim(clean, "/"), "/") {
		if strings.HasPrefix(segment, ".dfsha-stage-") {
			return "", false
		}
	}

	return clean, true
}

func cleanDirPath(p string) (string, bool) {
	if p == "" {
		return "/", true
	}

	if len(p) > 512 ||
		!strings.HasPrefix(p, "/") ||
		strings.ContainsRune(p, 0) {
		return "", false
	}

	return path.Clean(p), true
}
