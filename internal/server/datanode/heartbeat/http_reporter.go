package heartbeat

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"dfsha/internal/server/datanode/blockstore"
)

// ErrNotRegistered indica que el Metanode no conoce este DataNode.
var ErrNotRegistered = errors.New("datanode is not registered with the MetaNode")

type HTTPReporter struct {
	baseURL       string
	clusterSecret string
	client        *http.Client
}

func NewHTTPReporter(baseURL string) *HTTPReporter {
	return &HTTPReporter{
		baseURL: strings.TrimRight(baseURL, "/"),
		client: &http.Client{
			Timeout: 3 * time.Second,
		},
	}
}

// SetClusterSecret fija el secreto que autentica al DataNode ante el Metanode.
func (r *HTTPReporter) SetClusterSecret(secret string) {
	r.clusterSecret = secret
}

var _ Reporter = (*HTTPReporter)(nil)

type reportRequest struct {
	DataNodeID string                   `json:"datanode_id"`
	Address    string                   `json:"address"`
	FreeBytes  int64                    `json:"free_bytes"`
	BlockCount int                      `json:"block_count"`
	Blocks     []blockstore.BlockReport `json:"blocks"`
}

type heartbeatReply struct {
	Status       string   `json:"status"`
	DeleteBlocks []string `json:"delete_blocks"`
}

func (r *HTTPReporter) Register(ctx context.Context, report Report) error {
	_, err := r.post(ctx, "/internal/datanodes/register", report)
	return err
}

// Beat envía un latido y devuelve los bloques a borrar.
func (r *HTTPReporter) Beat(ctx context.Context, report Report) ([]string, error) {
	reply, err := r.post(ctx, "/internal/datanodes/heartbeat", report)
	if err != nil {
		return nil, err
	}
	return reply.DeleteBlocks, nil
}

// ReportBlock informa al Metanode que un bloque quedó almacenado (quórum).
func (r *HTTPReporter) ReportBlock(ctx context.Context, blockID, dataNodeID, sha256 string) error {
	body, err := json.Marshal(map[string]string{
		"block_id":    blockID,
		"datanode_id": dataNodeID,
		"sha256":      sha256,
	})
	if err != nil {
		return err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		r.baseURL+"/internal/blocks/report", bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	r.auth(req)

	resp, err := r.client.Do(req)
	if err != nil {
		return fmt.Errorf("connect to MetaNode: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("MetaNode returned HTTP %d while reporting block", resp.StatusCode)
	}
	return nil
}

func (r *HTTPReporter) post(
	ctx context.Context,
	endpoint string,
	report Report,
) (heartbeatReply, error) {
	payload := reportRequest{
		DataNodeID: report.DataNodeID,
		Address:    report.Address,
		FreeBytes:  report.FreeBytes,
		BlockCount: report.BlockCount,
		Blocks:     report.Blocks,
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return heartbeatReply{}, fmt.Errorf("marshal heartbeat: %w", err)
	}

	req, err := http.NewRequestWithContext(
		ctx,
		http.MethodPost,
		r.baseURL+endpoint,
		bytes.NewReader(body),
	)
	if err != nil {
		return heartbeatReply{}, fmt.Errorf("create request to MetaNode: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	r.auth(req)

	resp, err := r.client.Do(req)
	if err != nil {
		return heartbeatReply{}, fmt.Errorf("connect to MetaNode: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound {
		return heartbeatReply{}, ErrNotRegistered
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return heartbeatReply{}, fmt.Errorf("MetaNode returned HTTP %d", resp.StatusCode)
	}

	var reply heartbeatReply
	_ = json.NewDecoder(resp.Body).Decode(&reply)
	return reply, nil
}

func (r *HTTPReporter) auth(req *http.Request) {
	if r.clusterSecret != "" {
		req.Header.Set("X-Cluster-Secret", r.clusterSecret)
	}
}
