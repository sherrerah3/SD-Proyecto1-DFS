package client

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"

	"dfsha/common"
)

var ErrInvalidSession = errors.New("invalid or expired session")

func (c *Client) RestoreSession(token string) (Session, error) {
	if token == "" {
		return Session{}, ErrInvalidSession
	}

	req, err := http.NewRequest(
		http.MethodGet,
		c.baseURL+common.SessionPath,
		nil,
	)
	if err != nil {
		return Session{}, err
	}

	req.Header.Set("Authorization", "Bearer "+token)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return Session{}, fmt.Errorf("could not validate session: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusUnauthorized {
		return Session{}, ErrInvalidSession
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		message, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return Session{}, fmt.Errorf(
			"server returned %s: %s",
			resp.Status,
			strings.TrimSpace(string(message)),
		)
	}

	var result common.SessionInfoResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return Session{}, fmt.Errorf("invalid session response: %w", err)
	}

	if result.User == "" {
		return Session{}, fmt.Errorf("server returned an empty username")
	}

	return Session{Token: token, User: result.User}, nil
}
