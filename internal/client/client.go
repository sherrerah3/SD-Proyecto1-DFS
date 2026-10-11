package client

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"dfsha/common"
)

type Session struct {
	Token string
	User  string
}

type AuthService interface {
	Login(user, password string) (Session, error)
	Register(user, password string) (Session, error)
	RestoreSession(token string) (Session, error)

	ListDirectory(token, remotePath string) ([]common.DirectoryEntry, error)
	MakeDirectory(token, remotePath string) error
	RemoveDirectory(token, remotePath string) error
	RemoveDirectoryOptions(token, remotePath string, recursive, force bool) error
}

type Client struct {
	baseURL    string
	httpClient *http.Client
}

func New(baseURL string) *Client {
	return &Client{
		baseURL: strings.TrimRight(baseURL, "/"),
		httpClient: &http.Client{
			Timeout: 10 * time.Second,
		},
	}
}

func (c *Client) Login(user, password string) (Session, error) {
	return c.authenticate(common.LoginPath, user, password)
}

func (c *Client) Register(user, password string) (Session, error) {
	return c.authenticate(common.RegisterPath, user, password)
}

func (c *Client) authenticate(path, user, password string) (Session, error) {
	authRequest := common.AuthRequest{User: user, Password: password}
	if !common.ValidAuthRequest(authRequest) {
		return Session{}, fmt.Errorf("invalid user or password format")
	}

	body, err := json.Marshal(authRequest)
	if err != nil {
		return Session{}, err
	}
	req, err := http.NewRequest(http.MethodPost, c.baseURL+path, bytes.NewReader(body))
	if err != nil {
		return Session{}, err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return Session{}, fmt.Errorf("connection failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		message, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return Session{}, fmt.Errorf("server returned %s: %s", resp.Status, strings.TrimSpace(string(message)))
	}

	var result common.AuthResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return Session{}, fmt.Errorf("invalid server response: %w", err)
	}
	if result.Token == "" {
		return Session{}, fmt.Errorf("server returned an empty token")
	}
	return Session{Token: result.Token, User: user}, nil
}
