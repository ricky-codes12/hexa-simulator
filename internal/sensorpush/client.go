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
)

type Telemetry struct {
	HardwareID string
	DeviceTime time.Time
	Latitude   float64
	Longitude  float64
	Speed      float64
	Heading    float64
	Ignition   bool
	Movement   bool
}

type Client struct {
	URL     string
	Key     string
	Timeout time.Duration
}

type payload struct {
	Records []record `json:"records"`
}
type record struct {
	Schema string `json:"schema"`
	Device struct {
		HardwareID string `json:"hardware_id"`
	} `json:"device"`
	DeviceTime time.Time `json:"device_time"`
	Position   struct {
		FixValid bool    `json:"fix_valid"`
		Lat      float64 `json:"lat"`
		Lon      float64 `json:"lon"`
		Speed    float64 `json:"speed_kmh"`
		Heading  float64 `json:"heading_deg"`
	} `json:"position"`
	Attributes struct {
		Ignition bool `json:"ignition"`
		Movement bool `json:"movement"`
	} `json:"attributes"`
}

func (c Client) Send(ctx context.Context, t Telemetry) error {
	if strings.TrimSpace(c.URL) == "" || strings.TrimSpace(c.Key) == "" {
		return fmt.Errorf("push URL and key are required")
	}
	var r record
	r.Schema = "hexa.sensor/telemetry/v1"
	r.Device.HardwareID = t.HardwareID
	r.DeviceTime = t.DeviceTime
	r.Position.FixValid = true
	r.Position.Lat, r.Position.Lon = t.Latitude, t.Longitude
	r.Position.Speed, r.Position.Heading = t.Speed, t.Heading
	r.Attributes.Ignition, r.Attributes.Movement = t.Ignition, t.Movement
	body, err := json.Marshal(payload{Records: []record{r}})
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.URL, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+c.Key)
	req.Header.Set("Content-Type", "application/json")
	timeout := c.Timeout
	if timeout <= 0 {
		timeout = 5 * time.Second
	}
	resp, err := (&http.Client{Timeout: timeout}).Do(req)
	if err != nil {
		return fmt.Errorf("http push: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		return fmt.Errorf("http push status %s: %s", resp.Status, strings.TrimSpace(string(b)))
	}
	return nil
}
