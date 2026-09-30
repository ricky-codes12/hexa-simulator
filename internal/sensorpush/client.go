// Package sensorpush posts telemetry-v1 records to a Hexa.Sensor http-push source, as a vendor
// cloud would: POST {"records": [...]} with the source's ingest key as a Bearer token
// (Sensor's telemetry-v1 §4).
package sensorpush

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"hexa-simulator/internal/telemetry"
)

// MaxBatch is the most records one request carries; Sensor's default max_batch is 500.
const MaxBatch = 200

// Client posts to one http-push endpoint.
type Client struct {
	URL     string
	Key     string
	Timeout time.Duration
	HTTP    *http.Client
}

// Result is Sensor's answer: counts, and the records it rejected by their index in the batch.
type Result struct {
	Accepted  int           `json:"accepted"`
	Duplicate int           `json:"duplicate"`
	Rejected  int           `json:"rejected"`
	Errors    []RecordError `json:"errors"`
}

// RecordError is one rejected record: its index and Sensor's reason, such as unknown_device.
type RecordError struct {
	Index  int    `json:"index"`
	Reason string `json:"reason"`
	Detail string `json:"detail"`
}

// Send posts records in one request. A transport failure or a non-2xx status is an error for
// the whole batch; a record Sensor rejects is reported in the result.
func (c Client) Send(ctx context.Context, records []telemetry.Record) (Result, error) {
	if strings.TrimSpace(c.URL) == "" || strings.TrimSpace(c.Key) == "" {
		return Result{}, fmt.Errorf("push URL and key are required")
	}
	if len(records) == 0 {
		return Result{}, nil
	}
	body, err := json.Marshal(map[string]any{"records": records})
	if err != nil {
		return Result{}, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.URL, bytes.NewReader(body))
	if err != nil {
		return Result{}, err
	}
	req.Header.Set("Authorization", "Bearer "+c.Key)
	req.Header.Set("Content-Type", "application/json")
	client := c.HTTP
	if client == nil {
		timeout := c.Timeout
		if timeout <= 0 {
			timeout = 5 * time.Second
		}
		client = &http.Client{Timeout: timeout}
	}
	resp, err := client.Do(req)
	if err != nil {
		return Result{}, fmt.Errorf("http push: %w", err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return Result{}, fmt.Errorf("http push status %s: %s", resp.Status, strings.TrimSpace(string(raw[:min(len(raw), 300)])))
	}
	var res Result
	if len(raw) > 0 && json.Unmarshal(raw, &res) != nil {
		res = Result{Accepted: len(records)}
	}
	return res, nil
}
