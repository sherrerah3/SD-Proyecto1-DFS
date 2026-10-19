package api

import (
	"net/http"
	"time"
)

type FileLock struct {
	LockID     string    `json:"lock_id"`
	FenceToken uint64    `json:"fence_token"`
	ExpiresAt  time.Time `json:"expires_at"`
}

type lockRequest struct {
	Path       string `json:"path"`
	LockID     string `json:"lock_id,omitempty"`
	FenceToken uint64 `json:"fence_token,omitempty"`
}

// Lock requests an exclusive editing lease.
func (c *Client) Lock(remotePath, token string) (FileLock, error) {
	var result FileLock

	err := c.do(
		http.MethodPost,
		"/files/lock",
		token,
		lockRequest{Path: remotePath},
		&result,
	)

	return result, err
}

// Heartbeat renews the editing lease.
func (c *Client) Heartbeat(
	remotePath, token string,
	lock FileLock,
) (FileLock, error) {
	var result FileLock

	err := c.do(
		http.MethodPost,
		"/files/lock/heartbeat",
		token,
		lockRequest{
			Path:       remotePath,
			LockID:     lock.LockID,
			FenceToken: lock.FenceToken,
		},
		&result,
	)

	return result, err
}

// Unlock releases the editing lease.
func (c *Client) Unlock(
	remotePath, token string,
	lock FileLock,
) error {
	return c.do(
		http.MethodPost,
		"/files/lock/unlock",
		token,
		lockRequest{
			Path:       remotePath,
			LockID:     lock.LockID,
			FenceToken: lock.FenceToken,
		},
		nil,
	)
}
