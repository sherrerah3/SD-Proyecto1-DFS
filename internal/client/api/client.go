// Package api es el cliente HTTP (REST) del metanode.
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
)

const (
	// DFSHA_METANODE_URL acepta una o varias URLs separadas por coma.
	// Si el primer metanode no responde, se prueba el siguiente (alta disponibilidad).
	envMetanodeURL     = "DFSHA_METANODE_URL"
	defaultMetanodeURL = "http://localhost:8080"
	requestTimeout     = 10 * time.Second
)

// Error es una respuesta de error del metanode (HTTP 4xx o 5xx).
type Error struct {
	Status  int
	Message string
}

func (e *Error) Error() string {
	return fmt.Sprintf("%s (HTTP %d)", e.Message, e.Status)
}

// Client habla con el metanode por REST.
type Client struct {
	baseURLs []string
	http     *http.Client
}

type credentials struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

// LoginResult es la respuesta de POST /auth/login.
type LoginResult struct {
	Token     string `json:"token"`
	ExpiresIn int    `json:"expires_in"` // segundos
}

// New arma el cliente leyendo DFSHA_METANODE_URL (por defecto http://localhost:8080).
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

// Register crea un usuario nuevo (POST /auth/register, responde 204).
func (c *Client) Register(username, password string) error {
	return c.do(
		http.MethodPost,
		"/auth/register",
		"",
		credentials{Username: username, Password: password},
		nil,
	)
}

// Login inicia sesión (POST /auth/login) y devuelve el token JWT.
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

// do envía la petición al primer metanode que responda. Solo pasa al siguiente
// si no hubo conexión; cualquier respuesta HTTP (incluso un error) se devuelve tal cual.
func (c *Client) do(method, path, token string, body, out any) error {
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
		req, err := http.NewRequest(method, base+path, bytes.NewReader(payload))
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

	return fmt.Errorf("no se pudo contactar al metanode: %w", lastErr)
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
	putBlockSize = 1 << 20  // 1 MiB, igual que el metanode
	putChunkSize = 64 << 10 // 64 KiB por mensaje gRPC
)

type putInitRequest struct {
	Path      string `json:"path"`
	SizeBytes int64  `json:"size_bytes"`
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
	Path   string `json:"path"`
	SHA256 string `json:"sha256"`
}

// Put sube un archivo local al sistema de archivos DFSha.
func (c *Client) Put(localPath, remotePath, token string) error {
	info, err := os.Stat(localPath)
	if err != nil {
		return fmt.Errorf("no se puede acceder al archivo local: %w", err)
	}
	if !info.Mode().IsRegular() {
		return errors.New("la ruta local no corresponde a un archivo regular")
	}
	if info.Size() <= 0 {
		return errors.New("no se pueden subir archivos vacíos")
	}

	remotePath = path.Clean(remotePath)
	if !strings.HasPrefix(remotePath, "/") || remotePath == "/" {
		return errors.New("la ruta remota debe ser absoluta y apuntar a un archivo")
	}

	file, err := os.Open(localPath)
	if err != nil {
		return fmt.Errorf("no se pudo abrir el archivo: %w", err)
	}
	defer file.Close()

	var init putInitResponse
	if err := c.do(
		http.MethodPost,
		"/files/put-init",
		token,
		putInitRequest{
			Path:      remotePath,
			SizeBytes: info.Size(),
		},
		&init,
	); err != nil {
		return fmt.Errorf("el metanode rechazó el inicio de la subida: %w", err)
	}

	expectedBlocks := (info.Size() + putBlockSize - 1) / putBlockSize
	if int64(len(init.Blocks)) != expectedBlocks {
		return fmt.Errorf(
			"el metanode asignó %d bloques, pero se esperaban %d",
			len(init.Blocks),
			expectedBlocks,
		)
	}

	fileHash := sha256.New()
	remaining := info.Size()

	for i, block := range init.Blocks {
		if len(block.DataNodes) == 0 {
			return fmt.Errorf("el bloque %s no tiene datanodes asignados", block.BlockID)
		}

		blockSize := int64(putBlockSize)
		if remaining < blockSize {
			blockSize = remaining
		}

		data := make([]byte, int(blockSize))
		if _, err := io.ReadFull(file, data); err != nil {
			return fmt.Errorf("no se pudo leer el bloque %d: %w", i, err)
		}

		_, _ = fileHash.Write(data)

		blockHash := sha256.Sum256(data)

		if err := uploadBlock(
			block.BlockID,
			block.DataNodes,
			data,
			hex.EncodeToString(blockHash[:]),
		); err != nil {
			return fmt.Errorf("falló la subida del bloque %d (%s): %w", i, block.BlockID, err)
		}

		remaining -= blockSize
	}

	if remaining != 0 {
		return fmt.Errorf("quedaron %d bytes sin procesar", remaining)
	}

	if err := c.do(
		http.MethodPost,
		"/files/put-complete",
		token,
		putCompleteRequest{
			Path:   remotePath,
			SHA256: hex.EncodeToString(fileHash.Sum(nil)),
		},
		nil,
	); err != nil {
		return fmt.Errorf("los bloques se enviaron, pero no se pudo completar la subida: %w", err)
	}

	return nil
}

// uploadBlock envía un bloque al primer DataNode y le indica cuáles son sus réplicas.
func uploadBlock(
	blockID string,
	dataNodes []string,
	data []byte,
	blockHash string,
) error {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	conn, err := grpc.NewClient(
		dataNodes[0],
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	if err != nil {
		return fmt.Errorf("no se pudo conectar al datanode %s: %w", dataNodes[0], err)
	}
	defer conn.Close()

	client := datanodev1.NewBlockServiceClient(conn)

	stream, err := client.StoreBlock(ctx)
	if err != nil {
		return fmt.Errorf("no se pudo iniciar el envío gRPC: %w", err)
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
		return fmt.Errorf("no se pudo enviar el encabezado: %w", err)
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
			return fmt.Errorf("no se pudo enviar un fragmento: %w", err)
		}
	}

	response, err := stream.CloseAndRecv()
	if err != nil {
		return fmt.Errorf("el datanode no confirmó el bloque: %w", err)
	}

	if response.GetBlockId() != blockID {
		return fmt.Errorf("el datanode confirmó un bloque diferente: %s", response.GetBlockId())
	}

	if response.GetSha256() != blockHash {
		return errors.New("el hash del bloque no coincide con el confirmado por el datanode")
	}

	return nil
}

// FileEntry representa un archivo devuelto por el metanode.
type FileEntry struct {
	Name      string `json:"name"`
	Path      string `json:"path"`
	SizeBytes int64  `json:"size_bytes"`
	Owner     string `json:"owner"`
	Kind      string `json:"kind"`
}

// ListFiles obtiene los archivos del directorio indicado.
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

// ValidateSession comprueba que el token siga siendo válido.
func (c *Client) ValidateSession(token string) error {
	return c.do(
		http.MethodGet,
		"/auth/session",
		token,
		nil,
		&SessionResult{},
	)
}

// CreateDirectory crea un directorio remoto.
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

// DirectoryExists comprueba si existe un directorio remoto.
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
