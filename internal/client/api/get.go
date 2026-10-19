package api

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"

	datanodev1 "dfsha/internal/proto/datanode/v1"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

type getFileResponse struct {
	Blocks          []putBlock `json:"blocks"`
	SHA256          string     `json:"sha256"`
	CapabilityToken string     `json:"capability_token"`
}

// Get downloads a DFSha file and saves it to a local path.
func (c *Client) Get(remotePath, localPath, token string) error {
	return c.get(remotePath, localPath, token, nil)
}

// GetVerbose descarga un archivo e informa los bloques, DataNodes probados y fallos.
func (c *Client) GetVerbose(remotePath, localPath, token string, output io.Writer) error {
	if output == nil {
		output = io.Discard
	}
	return c.get(remotePath, localPath, token, output)
}

func (c *Client) get(remotePath, localPath, token string, output io.Writer) error {
	remotePath = path.Clean(remotePath)

	if !strings.HasPrefix(remotePath, "/") || remotePath == "/" {
		return errors.New("remote path must be absolute and identify a file")
	}

	if localPath == "" {
		return errors.New("local path cannot be empty")
	}

	// Encode each segment independently so slashes remain path separators.
	segments := strings.Split(strings.TrimPrefix(remotePath, "/"), "/")
	for i, segment := range segments {
		segments[i] = url.PathEscape(segment)
	}
	endpoint := "/files/" + strings.Join(segments, "/")

	var metadata getFileResponse
	if err := c.do(
		http.MethodGet,
		endpoint,
		token,
		nil,
		&metadata,
	); err != nil {
		return fmt.Errorf("MetaNode rejected the download request: %w", err)
	}

	if metadata.SHA256 == "" {
		return errors.New("MetaNode returned no file checksum")
	}

	// Create the temporary file in the destination directory so that
	// publishing it does not cross filesystem boundaries.
	destination := filepath.Clean(localPath)
	dir := filepath.Dir(destination)

	info, err := os.Stat(dir)
	if err != nil {
		return fmt.Errorf("cannot access destination directory: %w", err)
	}
	if !info.IsDir() {
		return errors.New("destination parent is not a directory")
	}

	temp, err := os.CreateTemp(dir, ".dfsha-get-*")
	if err != nil {
		return fmt.Errorf("cannot create temporary file: %w", err)
	}

	tempPath := temp.Name()
	defer os.Remove(tempPath)

	fileHash := sha256.New()

	for i, block := range metadata.Blocks {
		if len(block.DataNodes) == 0 {
			if output != nil {
				fmt.Fprintf(output, "[FAIL] block %d/%d ID=%s: the MetaNode did not assign any DataNodes\n",
					i+1, len(metadata.Blocks), block.BlockID)
			}
			temp.Close()
			return fmt.Errorf("block %s has no available DataNodes", block.BlockID)
		}

		data, source, err := downloadBlock(block.BlockID, block.DataNodes, metadata.CapabilityToken, output, i+1, len(metadata.Blocks))
		if err != nil {
			if output != nil {
				fmt.Fprintf(output, "[FAIL] could not retrieve block %d/%d ID=%s: %v\n",
					i+1, len(metadata.Blocks), block.BlockID, err)
			}
			temp.Close()
			return fmt.Errorf("failed to download block %d (%s): %w",
				i+1, block.BlockID, err)
		}
		if output != nil {
			fmt.Fprintf(output, "[OK] block %d/%d ID=%s retrieved from %s (%d bytes)\n",
				i+1, len(metadata.Blocks), block.BlockID, source, len(data))
		}

		if _, err := temp.Write(data); err != nil {
			temp.Close()
			return fmt.Errorf("cannot write downloaded block: %w", err)
		}

		_, _ = fileHash.Write(data)
	}

	actualHash := hex.EncodeToString(fileHash.Sum(nil))
	if !strings.EqualFold(actualHash, metadata.SHA256) {
		if output != nil {
			fmt.Fprintf(output, "[FAIL] file hash mismatch: expected=%s actual=%s\n",
				metadata.SHA256, actualHash)
		}
		temp.Close()
		return fmt.Errorf(
			"file checksum mismatch: expected %s, got %s",
			metadata.SHA256,
			actualHash,
		)
	}

	if err := temp.Sync(); err != nil {
		temp.Close()
		return fmt.Errorf("cannot flush downloaded file: %w", err)
	}

	if err := temp.Close(); err != nil {
		return fmt.Errorf("cannot close downloaded file: %w", err)
	}

	// Do not publish an incomplete or corrupted download.
	if err := publishDownloadedFile(tempPath, destination); err != nil {
		if output != nil {
			fmt.Fprintf(output, "[FAIL] could not save local file %s: %v\n", destination, err)
		}
		return fmt.Errorf("cannot save downloaded file: %w", err)
	}

	if output != nil {
		fmt.Fprintf(output, "[OK] file verified and saved to %s; SHA-256=%s\n", destination, actualHash)
	}
	return nil
}

// downloadBlock prueba cada réplica e informa los fallos y el origen exitoso.
func downloadBlock(blockID string, dataNodes []string, capability string, output io.Writer, blockNum, total int) ([]byte, string, error) {
	var lastErr error

	for attempt, address := range dataNodes {
		data, err := readBlockFromDataNode(address, blockID, capability)
		if err == nil {
			if output != nil && attempt > 0 {
				fmt.Fprintf(output, "  [RECOVERED] block %s: replica %s succeeded after %d failed attempt(s)\n",
					blockID, address, attempt)
			}
			return data, address, nil
		}

		lastErr = fmt.Errorf("%s: %w", address, err)
		if output != nil {
			fmt.Fprintf(output, "  [READ FAILURE] block %d/%d ID=%s on DataNode %s: %v\n",
				blockNum, total, blockID, address, err)
		}
	}

	if lastErr == nil {
		lastErr = errors.New("no replicas are available")
	}
	return nil, "", fmt.Errorf("all replicas failed: %w", lastErr)
}

func readBlockFromDataNode(address, blockID, capability string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(
		context.Background(),
		2*time.Minute,
	)
	defer cancel()
	ctx = withCapability(ctx, capability)

	conn, err := grpc.NewClient(
		address,
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	if err != nil {
		return nil, fmt.Errorf("cannot connect: %w", err)
	}
	defer conn.Close()

	client := datanodev1.NewBlockServiceClient(conn)

	stream, err := client.GetBlock(ctx, &datanodev1.GetBlockRequest{
		BlockId: blockID,
	})
	if err != nil {
		return nil, fmt.Errorf("cannot request block: %w", err)
	}

	var data bytes.Buffer
	var headerReceived bool
	var expectedSize int64
	var expectedHash string

	for {
		message, err := stream.Recv()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("stream failed: %w", err)
		}

		switch payload := message.GetPayload().(type) {
		case *datanodev1.GetBlockResponse_Header:
			if headerReceived {
				return nil, errors.New("received duplicate block header")
			}

			header := payload.Header
			if header.GetBlockId() != blockID {
				return nil, errors.New("DataNode returned a different block")
			}
			if header.GetSizeBytes() < 0 {
				return nil, errors.New("invalid block size")
			}

			headerReceived = true
			expectedSize = header.GetSizeBytes()
			expectedHash = header.GetSha256()

		case *datanodev1.GetBlockResponse_Chunk:
			if !headerReceived {
				return nil, errors.New("received block data before header")
			}
			if int64(data.Len())+int64(len(payload.Chunk)) > expectedSize {
				return nil, errors.New("block exceeds declared size")
			}

			_, _ = data.Write(payload.Chunk)

		default:
			return nil, errors.New("received an unknown block message")
		}
	}

	if !headerReceived {
		return nil, errors.New("DataNode returned no block header")
	}

	result := data.Bytes()

	if int64(len(result)) != expectedSize {
		return nil, fmt.Errorf(
			"incomplete block: expected %d bytes, received %d",
			expectedSize,
			len(result),
		)
	}

	hash := sha256.Sum256(result)
	actualHash := hex.EncodeToString(hash[:])

	if expectedHash == "" || !strings.EqualFold(actualHash, expectedHash) {
		return nil, fmt.Errorf(
			"block checksum mismatch: expected=%q actual=%q size=%d",
			expectedHash,
			actualHash,
			len(result),
		)
	}

	return result, nil
}

// publishDownloadedFile replaces the destination only after verification.
func publishDownloadedFile(tempPath, destination string) error {
	// On systems where rename replaces an existing file, this is atomic.
	if err := os.Rename(tempPath, destination); err == nil {
		return nil
	} else {
		// Windows may refuse to rename over an existing destination.
		// Keep the original recoverable until the new file is in place.
		if _, statErr := os.Lstat(destination); statErr != nil {
			return err
		}

		backup, backupErr := os.CreateTemp(
			filepath.Dir(destination),
			".dfsha-backup-*",
		)
		if backupErr != nil {
			return err
		}
		backupPath := backup.Name()
		if closeErr := backup.Close(); closeErr != nil {
			os.Remove(backupPath)
			return closeErr
		}
		if removeErr := os.Remove(backupPath); removeErr != nil {
			return removeErr
		}

		if backupErr := os.Rename(destination, backupPath); backupErr != nil {
			return fmt.Errorf("cannot preserve existing destination: %w", backupErr)
		}

		if replaceErr := os.Rename(tempPath, destination); replaceErr != nil {
			if rollbackErr := os.Rename(backupPath, destination); rollbackErr != nil {
				return fmt.Errorf(
					"replacement failed: %v; restoration failed: %w",
					replaceErr,
					rollbackErr,
				)
			}
			return replaceErr
		}

		if removeErr := os.Remove(backupPath); removeErr != nil {
			return fmt.Errorf("file downloaded, but backup cleanup failed: %w", removeErr)
		}
		return nil
	}
}
