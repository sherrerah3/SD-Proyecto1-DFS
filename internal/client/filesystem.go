package client

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"

	"dfsha/common"
)

func (c *Client) ListDirectory(token, remotePath string) ([]common.DirectoryEntry, error) {
	query := url.Values{}
	query.Set("path", remotePath)
	req, err := http.NewRequest(http.MethodGet, c.baseURL+common.DirectoriesPath+"?"+query.Encode(), nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+token)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("connection failed: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		message, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return nil, fmt.Errorf("server returned %s: %s", resp.Status, strings.TrimSpace(string(message)))
	}
	var entries []common.DirectoryEntry
	if err := json.NewDecoder(resp.Body).Decode(&entries); err != nil {
		return nil, fmt.Errorf("invalid directory response: %w", err)
	}
	return entries, nil
}

func (c *Client) MakeDirectory(token, remotePath string) error {
	body, err := json.Marshal(common.CreateDirectoryRequest{Path: remotePath})
	if err != nil {
		return err
	}
	req, err := http.NewRequest(http.MethodPost, c.baseURL+common.DirectoriesPath, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("connection failed: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		message, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return fmt.Errorf("server returned %s: %s", resp.Status, strings.TrimSpace(string(message)))
	}
	return nil
}

func (c *Client) RemoveDirectory(token, remotePath string) error {
	return c.RemoveDirectoryOptions(token, remotePath, false, false)
}

func (c *Client) RemoveDirectoryOptions(token, remotePath string, recursive, force bool) error {
	query := url.Values{}
	query.Set("path", remotePath)
	query.Set("recursive", fmt.Sprintf("%t", recursive))
	query.Set("force", fmt.Sprintf("%t", force))

	req, err := http.NewRequest(http.MethodDelete, c.baseURL+common.DirectoriesPath+"?"+query.Encode(), nil)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+token)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("connection failed: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		message, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return fmt.Errorf("server returned %s: %s", resp.Status, strings.TrimSpace(string(message)))
	}
	return nil
}
