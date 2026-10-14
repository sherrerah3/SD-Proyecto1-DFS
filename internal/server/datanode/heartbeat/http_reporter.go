package heartbeat

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"
)

type HTTPReporter struct {
	baseURL string
	client  *http.Client
}

func NewHTTPReporter(baseURL string) *HTTPReporter {
	return &HTTPReporter{
		baseURL: strings.TrimRight(baseURL, "/"),
		client: &http.Client{
			Timeout: 3 * time.Second,
		},
	}
}

var _ Reporter = (*HTTPReporter)(nil)

type reportRequest struct {
	DataNodeID string `json:"datanode_id"`
	Address    string `json:"address"`
	FreeBytes  int64  `json:"free_bytes"`
	BlockCount int    `json:"block_count"`
}

func (r *HTTPReporter) Register(ctx context.Context, report Report) error {
	return r.post(ctx, "/internal/datanodes/register", report)
}

func (r *HTTPReporter) Beat(ctx context.Context, report Report) error {
	return r.post(ctx, "/internal/datanodes/heartbeat", report)
}

func (r *HTTPReporter) post(
	ctx context.Context,
	endpoint string,
	report Report,
) error {
	payload := reportRequest{
		DataNodeID: report.DataNodeID,
		Address:    report.Address,
		FreeBytes:  report.FreeBytes,
		BlockCount: report.BlockCount,
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("serializar heartbeat: %w", err)
	}

	req, err := http.NewRequestWithContext(
		ctx,
		http.MethodPost,
		r.baseURL+endpoint,
		bytes.NewReader(body),
	)
	if err != nil {
		return fmt.Errorf("crear petición al metanode: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := r.client.Do(req)
	if err != nil {
		return fmt.Errorf("conectar con el metanode: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("metanode respondió HTTP %d", resp.StatusCode)
	}

	return nil
}
