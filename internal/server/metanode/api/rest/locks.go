package rest

import (
	"net/http"
	"path"
	"time"

	"dfsha/internal/server/metanode/service"

	"github.com/google/uuid"
)

const fileLockTTL = 30 * time.Second

type fileLock struct {
	Username   string
	Path       string
	ID         string
	FenceToken uint64
	ExpiresAt  time.Time
}

type lockRequest struct {
	Path       string `json:"path"`
	LockID     string `json:"lock_id"`
	FenceToken uint64 `json:"fence_token"`
}

type lockResponse struct {
	LockID     string    `json:"lock_id"`
	FenceToken uint64    `json:"fence_token"`
	ExpiresAt  time.Time `json:"expires_at"`
}

func lockKey(username, filePath string) string {
	return username + "\x00" + path.Clean(filePath)
}

// handleLock acquires an exclusive lease for an existing remote file.
func (srv *Server) handleLock(w http.ResponseWriter, r *http.Request) {
	username := usernameFrom(r)

	var req lockRequest
	if !decodeJSON(w, r, &req) {
		return
	}

	filePath, ok := cleanFilePath(req.Path)
	if !ok {
		writeError(w, http.StatusBadRequest, "invalid file path")
		return
	}

	// Do not lock a nonexistent file.
	if _, _, err := service.ResolveGet(srv.store, username, filePath); err != nil {
		writeServiceError(w, err)
		return
	}

	now := time.Now()
	key := lockKey(username, filePath)

	srv.lockMu.Lock()
	defer srv.lockMu.Unlock()

	if current, exists := srv.locks[key]; exists && now.Before(current.ExpiresAt) {
		writeError(w, http.StatusConflict, "file is already being edited")
		return
	}

	srv.nextFence++

	lock := &fileLock{
		Username:   username,
		Path:       filePath,
		ID:         uuid.NewString(),
		FenceToken: srv.nextFence,
		ExpiresAt:  now.Add(fileLockTTL),
	}
	srv.locks[key] = lock

	writeJSON(w, http.StatusOK, lockResponse{
		LockID:     lock.ID,
		FenceToken: lock.FenceToken,
		ExpiresAt:  lock.ExpiresAt,
	})
}

// handleLockHeartbeat renews an active lease.
func (srv *Server) handleLockHeartbeat(w http.ResponseWriter, r *http.Request) {
	username := usernameFrom(r)

	var req lockRequest
	if !decodeJSON(w, r, &req) {
		return
	}

	filePath, ok := cleanFilePath(req.Path)
	if !ok {
		writeError(w, http.StatusBadRequest, "invalid file path")
		return
	}

	srv.lockMu.Lock()
	defer srv.lockMu.Unlock()

	lock, exists := srv.locks[lockKey(username, filePath)]
	if !exists ||
		time.Now().After(lock.ExpiresAt) ||
		lock.ID != req.LockID ||
		lock.FenceToken != req.FenceToken {
		writeError(w, http.StatusConflict, "file lock expired or lost")
		return
	}

	lock.ExpiresAt = time.Now().Add(fileLockTTL)

	writeJSON(w, http.StatusOK, lockResponse{
		LockID:     lock.ID,
		FenceToken: lock.FenceToken,
		ExpiresAt:  lock.ExpiresAt,
	})
}

// handleUnlock releases the lease only if the caller still owns it.
func (srv *Server) handleUnlock(w http.ResponseWriter, r *http.Request) {
	username := usernameFrom(r)

	var req lockRequest
	if !decodeJSON(w, r, &req) {
		return
	}

	filePath, ok := cleanFilePath(req.Path)
	if !ok {
		writeError(w, http.StatusBadRequest, "invalid file path")
		return
	}

	srv.lockMu.Lock()
	defer srv.lockMu.Unlock()

	key := lockKey(username, filePath)
	lock, exists := srv.locks[key]
	if !exists {
		w.WriteHeader(http.StatusNoContent)
		return
	}

	if lock.ID != req.LockID ||
		lock.FenceToken != req.FenceToken {
		writeError(w, http.StatusConflict, "file lock belongs to another editor")
		return
	}

	delete(srv.locks, key)
	w.WriteHeader(http.StatusNoContent)
}

// validateWriteLock prevents a regular PUT from modifying a locked file.
// A locked PUT must present the current lock ID and fencing token.
func (srv *Server) validateWriteLock(
	username, filePath, lockID string,
	fenceToken uint64,
) error {
	key := lockKey(username, filePath)
	now := time.Now()

	srv.lockMu.Lock()
	defer srv.lockMu.Unlock()

	lock, exists := srv.locks[key]
	if !exists {
		if lockID != "" {
			return errFileLockLost
		}
		return nil
	}

	if !now.Before(lock.ExpiresAt) {
		delete(srv.locks, key)
		if lockID != "" {
			return errFileLockLost
		}
		return nil
	}

	if lockID == "" ||
		lock.ID != lockID ||
		lock.FenceToken != fenceToken {
		return errFileLocked
	}

	return nil
}

var (
	errFileLocked   = &lockError{"file is locked by another editor"}
	errFileLockLost = &lockError{"file lock expired or lost"}
)

type lockError struct {
	message string
}

func (e *lockError) Error() string {
	return e.message
}
