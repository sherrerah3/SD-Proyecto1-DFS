// Package api is the HTTP (REST) client for the metanode.
package api

import (
	"bytes"
	"context"
	"crypto/sha256"
	datanodev1 "dfsha/internal/proto/datanode/v1"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path"
	"strings"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"
)

const (
	// DFSHA_METANODE_URL accepts one or more comma-separated URLs.
	// If the first metanode is unavailable, the next one is tried.
	envMetanodeURL     = "DFSHA_METANODE_URL"
	defaultMetanodeURL = "http://localhost:8080"
	requestTimeout     = 10 * time.Second
)

// Error represents an error response from the metanode (HTTP 4xx or 5xx).
type Error struct {
	Status  int
	Message string
}

func (e *Error) Error() string {
	return fmt.Sprintf("%s (HTTP %d)", e.Message, e.Status)
}

// Client communicates with the metanode through REST.
type Client struct {
	baseURLs []string
	http     *http.Client
}

type credentials struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

// LoginResult is the response from POST /auth/login.
type LoginResult struct {
	Token     string `json:"token"`
	ExpiresIn int    `json:"expires_in"` // seconds
}

// New creates the client using DFSHA_METANODE_URL.
func New() *Client {
	raw := os.Getenv(envMetanodeURL)
	if strings.TrimSpace(raw) == "" {
		raw = defaultMetanodeURL
	}

	urls := make([]string, 0, 2)

	for _, u := range strings.Split(raw, ",") {
		u = strings.TrimRight(strings.TrimSpace(u), "/")
		if u != "" {
			urls = append(urls, u)
		}
	}

	if len(urls) == 0 {
		urls = []string{defaultMetanodeURL}
	}

	return &Client{
		baseURLs: urls,
		http:     &http.Client{Timeout: requestTimeout},
	}
}

// Register creates a new user (POST /auth/register, responds with 204).
func (c *Client) Register(username, password string) error {
	return c.do(
		http.MethodPost,
		"/auth/register",
		"",
		credentials{Username: username, Password: password},
		nil,
	)
}

// Login authenticates a user (POST /auth/login) and returns the JWT.
func (c *Client) Login(username, password string) (LoginResult, error) {
	var result LoginResult

	err := c.do(
		http.MethodPost,
		"/auth/login",
		"",
		credentials{Username: username, Password: password},
		&result,
	)

	return result, err
}

// do sends the request to the first metanode that responds.
// It tries the next one only if the connection fails.
func (c *Client) do(method, endpoint, token string, body, out any) error {
	var payload []byte

	if body != nil {
		var err error

		payload, err = json.Marshal(body)
		if err != nil {
			return err
		}
	}

	var lastErr error

	for _, base := range c.baseURLs {
		req, err := http.NewRequest(method, base+endpoint, bytes.NewReader(payload))
		if err != nil {
			return err
		}

		if body != nil {
			req.Header.Set("Content-Type", "application/json")
		}

		if token != "" {
			req.Header.Set("Authorization", "Bearer "+token)
		}

		resp, err := c.http.Do(req)
		if err != nil {
			lastErr = err
			continue
		}

		return decodeResponse(resp, out)
	}

	return fmt.Errorf("could not contact the MetaNode: %w", lastErr)
}

func decodeResponse(resp *http.Response, out any) error {
	defer resp.Body.Close()

	data, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return err
	}

	if resp.StatusCode >= 400 {
		message := strings.TrimSpace(string(data))

		var apiErr struct {
			Error string `json:"error"`
		}

		if json.Unmarshal(data, &apiErr) == nil && apiErr.Error != "" {
			message = apiErr.Error
		}

		if message == "" {
			message = http.StatusText(resp.StatusCode)
		}

		return &Error{Status: resp.StatusCode, Message: message}
	}

	if out == nil || len(data) == 0 {
		return nil
	}

	return json.Unmarshal(data, out)
}

const (
	putBlockSize = 1 << 20  // 1 MiB, matching the metanode.
	putChunkSize = 64 << 10 // 64 KiB per gRPC message.
)

type putInitRequest struct {
	Path       string `json:"path"`
	SizeBytes  int64  `json:"size_bytes"`
	LockID     string `json:"lock_id,omitempty"`
	FenceToken uint64 `json:"fence_token,omitempty"`
}

type putBlock struct {
	BlockID   string   `json:"block_id"`
	DataNodes []string `json:"datanodes"`
}

type putInitResponse struct {
	Blocks          []putBlock `json:"blocks"`
	CapabilityToken string     `json:"capability_token"`
}

type putCompleteRequest struct {
	Path       string `json:"path"`
	SHA256     string `json:"sha256"`
	LockID     string `json:"lock_id,omitempty"`
	FenceToken uint64 `json:"fence_token,omitempty"`
}

// Put uploads a local file to DFSha.
func (c *Client) Put(localPath, remotePath, token string) error {
	return c.put(localPath, remotePath, token, nil, "", 0)
}

// PutWithLock uploads a file using a valid editing lease.
func (c *Client) PutWithLock(
	localPath, remotePath, token string,
	lock FileLock,
) error {
	return c.put(
		localPath,
		remotePath,
		token,
		nil,
		lock.LockID,
		lock.FenceToken,
	)
}

// PutVerbose uploads a file and reports which DataNodes stored each block.
func (c *Client) PutVerbose(localPath, remotePath, token string, output io.Writer) error {
	if output == nil {
		output = io.Discard
	}
	return c.put(localPath, remotePath, token, output, "", 0)
}

func (c *Client) put(
	localPath, remotePath, token string,
	output io.Writer,
	lockID string,
	fenceToken uint64,
) error {
	info, err := os.Stat(localPath)
	if err != nil {
		return fmt.Errorf("cannot access local file: %w", err)
	}
	if !info.Mode().IsRegular() {
		return errors.New("local path does not refer to a regular file")
	}
	if info.Size() <= 0 {
		return errors.New("cannot upload empty files")
	}

	remotePath = path.Clean(remotePath)
	if !strings.HasPrefix(remotePath, "/") || remotePath == "/" {
		return errors.New("remote path must be absolute and point to a file")
	}

	file, err := os.Open(localPath)
	if err != nil {
		return fmt.Errorf("failed to open file: %w", err)
	}
	defer file.Close()

	var init putInitResponse
	if err := c.do(
		http.MethodPost,
		"/files/put-init",
		token,
		putInitRequest{
			Path:       remotePath,
			SizeBytes:  info.Size(),
			LockID:     lockID,
			FenceToken: fenceToken,
		},
		&init,
	); err != nil {
		return fmt.Errorf("MetaNode rejected the upload initialization: %w", err)
	}

	expectedBlocks := (info.Size() + putBlockSize - 1) / putBlockSize
	if int64(len(init.Blocks)) != expectedBlocks {
		return fmt.Errorf(
			"MetaNode assigned %d blocks, but expected %d",
			len(init.Blocks),
			expectedBlocks,
		)
	}

	fileHash := sha256.New()
	remaining := info.Size()

	for i, block := range init.Blocks {
		if len(block.DataNodes) == 0 {
			return fmt.Errorf("block %s has no assigned DataNodes", block.BlockID)
		}

		blockSize := int64(putBlockSize)
		if remaining < blockSize {
			blockSize = remaining
		}

		data := make([]byte, int(blockSize))
		if _, err := io.ReadFull(file, data); err != nil {
			return fmt.Errorf("failed to read block %d: %w", i+1, err)
		}

		_, _ = fileHash.Write(data)

		blockHash := sha256.Sum256(data)

		storedOn, uploadErr := uploadBlock(
			block.BlockID,
			block.DataNodes,
			data,
			hex.EncodeToString(blockHash[:]),
			init.CapabilityToken,
		)

		// The DataNode protocol reports its own successful storage as "self".
		// Translate that marker to the primary DataNode address assigned by the metanode.
		for j, address := range storedOn {
			if address == "self" && len(block.DataNodes) > 0 {
				storedOn[j] = block.DataNodes[0]
			}
		}

		if output != nil {
			fmt.Fprintf(output, "[put] block %d/%d ID=%s size=%d bytes\n",
				i+1, len(init.Blocks), block.BlockID, len(data))
			confirmed := make(map[string]bool, len(storedOn))
			for _, address := range storedOn {
				confirmed[address] = true
			}
			for _, address := range block.DataNodes {
				switch {
				case confirmed[address]:
					fmt.Fprintf(output, "  [OK] stored on DataNode %s\n", address)
				case uploadErr != nil && address == block.DataNodes[0]:
					fmt.Fprintf(output, "  [FAIL] primary DataNode %s: %v\n", address, uploadErr)
				default:
					fmt.Fprintf(output, "  [UNCONFIRMED] DataNode %s is missing from the server confirmation\n", address)
				}
			}
		}

		if uploadErr != nil {
			return fmt.Errorf("failed to upload block %d (%s): %w", i+1, block.BlockID, uploadErr)
		}
		for _, address := range block.DataNodes {
			found := false
			for _, stored := range storedOn {
				if stored == address {
					found = true
					break
				}
			}
			if !found {
				return fmt.Errorf("block %s was accepted, but DataNode %s did not confirm storage", block.BlockID, address)
			}
		}

		remaining -= blockSize
	}

	if remaining != 0 {
		return fmt.Errorf("%d bytes remain unprocessed", remaining)
	}

	if output != nil {
		fmt.Fprintln(output, "[put] all blocks confirmed; finalizing metadata on the MetaNode")
	}

	if err := c.do(
		http.MethodPost,
		"/files/put-complete",
		token,
		putCompleteRequest{
			Path:       remotePath,
			SHA256:     hex.EncodeToString(fileHash.Sum(nil)),
			LockID:     lockID,
			FenceToken: fenceToken,
		},
		nil,
	); err != nil {
		if output != nil {
			fmt.Fprintf(output, "[FAIL] MetaNode could not complete the upload: %v\n", err)
		}
		return fmt.Errorf("blocks were sent, but the upload could not be completed: %w", err)
	}

	if output != nil {
		fmt.Fprintf(output, "[OK] file committed to DFSha: %s\n", remotePath)
	}
	return nil
}

// uploadBlock sends a block to the first DataNode and returns the DataNodes
// that the server confirms stored the block.
func uploadBlock(
	blockID string,
	dataNodes []string,
	data []byte,
	blockHash string,
	capability string,
) ([]string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	ctx = withCapability(ctx, capability)

	conn, err := grpc.NewClient(
		dataNodes[0],
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	if err != nil {
		return nil, fmt.Errorf("could not connect to DataNode %s: %w", dataNodes[0], err)
	}
	defer conn.Close()

	client := datanodev1.NewBlockServiceClient(conn)

	stream, err := client.StoreBlock(ctx)
	if err != nil {
		return nil, fmt.Errorf("could not start gRPC upload: %w", err)
	}

	header := &datanodev1.StoreBlockRequest{
		Payload: &datanodev1.StoreBlockRequest_Header{
			Header: &datanodev1.StoreBlockHeader{
				BlockId:        blockID,
				Sha256:         blockHash,
				SizeBytes:      int64(len(data)),
				ReplicaTargets: dataNodes[1:],
			},
		},
	}

	if err := stream.Send(header); err != nil {
		return nil, fmt.Errorf("could not send block header: %w", err)
	}

	for offset := 0; offset < len(data); offset += putChunkSize {
		end := offset + putChunkSize
		if end > len(data) {
			end = len(data)
		}

		chunk := &datanodev1.StoreBlockRequest{
			Payload: &datanodev1.StoreBlockRequest_Chunk{
				Chunk: data[offset:end],
			},
		}

		if err := stream.Send(chunk); err != nil {
			return nil, fmt.Errorf("could not send data chunk: %w", err)
		}
	}

	response, err := stream.CloseAndRecv()
	if err != nil {
		return nil, fmt.Errorf("DataNode did not confirm the block: %w", err)
	}

	if response.GetBlockId() != blockID {
		return nil, fmt.Errorf("DataNode confirmed a different block: %s", response.GetBlockId())
	}

	if response.GetSha256() != blockHash {
		return nil, errors.New("block hash does not match the hash confirmed by the DataNode")
	}

	return response.GetStoredOn(), nil
}

// FileEntry represents a file returned by the metanode.
type FileEntry struct {
	Name      string `json:"name"`
	Path      string `json:"path"`
	SizeBytes int64  `json:"size_bytes"`
	Owner     string `json:"owner"`
	Kind      string `json:"kind"`
}

// ListFiles retrieves the files in the specified directory.
func (c *Client) ListFiles(dir, token string) ([]FileEntry, error) {
	var entries []FileEntry

	endpoint := "/files?dir=" + url.QueryEscape(dir)

	err := c.do(
		http.MethodGet,
		endpoint,
		token,
		nil,
		&entries,
	)
	if err != nil {
		return nil, err
	}

	return entries, nil
}

type SessionResult struct {
	Username string `json:"username"`
}

// ValidateSession checks whether the token is still valid.
func (c *Client) ValidateSession(token string) error {
	return c.do(
		http.MethodGet,
		"/auth/session",
		token,
		nil,
		&SessionResult{},
	)
}

// CreateDirectory creates a remote directory.
func (c *Client) CreateDirectory(dirPath, token string) error {
	return c.do(
		http.MethodPost,
		"/directories",
		token,
		struct {
			Path string `json:"path"`
		}{Path: dirPath},
		nil,
	)
}

// DirectoryExists checks whether a remote directory exists.
func (c *Client) DirectoryExists(dirPath, token string) error {
	endpoint := "/directories/exists?path=" + url.QueryEscape(dirPath)

	return c.do(
		http.MethodGet,
		endpoint,
		token,
		nil,
		&struct {
			Exists bool `json:"exists"`
		}{},
	)
}

func (c *Client) RemoveFile(remotePath, token string) error {
	remotePath = path.Clean(remotePath)
	if !strings.HasPrefix(remotePath, "/") || remotePath == "/" {
		return errors.New("remote path must identify a file")
	}

	segments := strings.Split(strings.TrimPrefix(remotePath, "/"), "/")
	for i, segment := range segments {
		segments[i] = url.PathEscape(segment)
	}
	endpoint := "/files/" + strings.Join(segments, "/")

	// The metanode schedules the block deletion on the DataNodes.
	if err := c.do(http.MethodDelete, endpoint, token, nil, nil); err != nil {
		return fmt.Errorf("cannot remove file metadata: %w", err)
	}

	return nil
}

func (c *Client) RemoveDirectory(remotePath, token string) error {
	remotePath = path.Clean(remotePath)
	if !strings.HasPrefix(remotePath, "/") || remotePath == "/" {
		return errors.New("remote path must identify a directory other than root")
	}

	endpoint := "/directories?path=" + url.QueryEscape(remotePath)
	return c.do(http.MethodDelete, endpoint, token, nil, nil)
}

// withCapability adds the capability token to the outgoing gRPC metadata.
func withCapability(ctx context.Context, token string) context.Context {
	return metadata.AppendToOutgoingContext(ctx, "authorization", "Bearer "+token)
}
