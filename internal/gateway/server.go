package gateway

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	"hexa-simulator/internal/teltonika"
)

type Config struct {
	ListenAddress string
	SensorURL     string
	SecretKey     string
	Timeout       time.Duration
}

type NormalizedTelemetry struct {
	DeviceID  string    `json:"device_id"`
	Timestamp time.Time `json:"timestamp"`
	Location  Location  `json:"location"`
	Speed     float64   `json:"speed"`
	Heading   float64   `json:"heading"`
	Ignition  bool      `json:"ignition"`
	Status    string    `json:"status"`
	Protocol  string    `json:"protocol"`
}

type Location struct {
	Latitude  float64 `json:"latitude"`
	Longitude float64 `json:"longitude"`
}

type Server struct {
	cfg      Config
	client   *http.Client
	listener net.Listener
	wg       sync.WaitGroup
}

func New(cfg Config) (*Server, error) {
	cfg.ListenAddress = strings.TrimSpace(cfg.ListenAddress)
	cfg.SensorURL = strings.TrimSpace(cfg.SensorURL)
	if cfg.ListenAddress == "" {
		return nil, fmt.Errorf("gateway listen address is required")
	}
	if cfg.SensorURL == "" {
		return nil, fmt.Errorf("HEXA.SENSOR URL is required")
	}
	if strings.TrimSpace(cfg.SecretKey) == "" {
		return nil, fmt.Errorf("HEXA.SENSOR secret key is required")
	}
	if cfg.Timeout <= 0 {
		cfg.Timeout = 5 * time.Second
	}
	return &Server{cfg: cfg, client: &http.Client{Timeout: cfg.Timeout}}, nil
}

func (s *Server) Start(ctx context.Context) error {
	listener, err := net.Listen("tcp", s.cfg.ListenAddress)
	if err != nil {
		return fmt.Errorf("listen Teltonika gateway: %w", err)
	}
	s.listener = listener
	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		<-ctx.Done()
		_ = listener.Close()
	}()
	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		for {
			conn, err := listener.Accept()
			if err != nil {
				if ctx.Err() != nil {
					return
				}
				continue
			}
			s.wg.Add(1)
			go func() { defer s.wg.Done(); defer conn.Close(); _ = s.handle(ctx, conn) }()
		}
	}()
	return nil
}

func (s *Server) Addr() string {
	if s.listener == nil {
		return ""
	}
	return s.listener.Addr().String()
}

func (s *Server) Wait() { s.wg.Wait() }

func (s *Server) handle(ctx context.Context, conn net.Conn) error {
	_ = conn.SetDeadline(time.Now().Add(s.cfg.Timeout))
	var imeiLength [2]byte
	if _, err := io.ReadFull(conn, imeiLength[:]); err != nil {
		return err
	}
	length := int(binary.BigEndian.Uint16(imeiLength[:]))
	if length != 15 {
		_, _ = conn.Write([]byte{0})
		return fmt.Errorf("invalid IMEI length")
	}
	imeiBytes := make([]byte, length)
	if _, err := io.ReadFull(conn, imeiBytes); err != nil {
		return err
	}
	imei := string(imeiBytes)
	if _, err := teltonika.EncodeIMEI(imei); err != nil {
		_, _ = conn.Write([]byte{0})
		return err
	}
	if _, err := conn.Write([]byte{1}); err != nil {
		return err
	}

	var header [8]byte
	if _, err := io.ReadFull(conn, header[:]); err != nil {
		return err
	}
	dataLength := int(binary.BigEndian.Uint32(header[4:8]))
	if binary.BigEndian.Uint32(header[:4]) != 0 || dataLength <= 0 || dataLength > 64*1024 {
		return fmt.Errorf("invalid AVL frame header")
	}
	bodyAndCRC := make([]byte, dataLength+4)
	if _, err := io.ReadFull(conn, bodyAndCRC); err != nil {
		return err
	}
	packet := append(append([]byte(nil), header[:]...), bodyAndCRC...)
	record, err := teltonika.DecodeCodec8Extended(packet)
	if err != nil {
		return err
	}
	if err := s.forward(ctx, imei, record); err != nil {
		return err
	}
	var ack [4]byte
	binary.BigEndian.PutUint32(ack[:], 1)
	_, err = conn.Write(ack[:])
	return err
}

func (s *Server) forward(ctx context.Context, imei string, record teltonika.Record) error {
	payload := NormalizedTelemetry{
		DeviceID:  imei,
		Timestamp: record.Timestamp,
		Location:  Location{Latitude: record.Latitude, Longitude: record.Longitude},
		Speed:     record.Speed,
		Heading:   record.Heading,
		Ignition:  record.Ignition,
		Status:    "online",
		Protocol:  "teltonika-codec8e",
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, s.cfg.SensorURL, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("create HEXA.SENSOR request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-SECRET-KEY", s.cfg.SecretKey)
	response, err := s.client.Do(req)
	if err != nil {
		return fmt.Errorf("forward to HEXA.SENSOR: %w", err)
	}
	defer response.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, 4096))
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return fmt.Errorf("HEXA.SENSOR returned %s", response.Status)
	}
	return nil
}
