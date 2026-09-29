package teltonika

import (
	"context"
	"encoding/binary"
	"fmt"
	"io"
	"net"
	"strings"
	"sync"
	"time"
)

type Client struct {
	Address string
	Timeout time.Duration
	Codec   string
	mu      sync.Mutex
	conns   map[string]*deviceConn
}

type deviceConn struct {
	mu   sync.Mutex
	conn net.Conn
}
type Telemetry struct {
	IMEI                                string
	Timestamp                           time.Time
	Latitude, Longitude, Speed, Heading float64
	Ignition                            bool
}

func (c *Client) Close() {
	c.mu.Lock()
	entries := c.conns
	c.conns = nil
	c.mu.Unlock()
	for _, entry := range entries {
		entry.mu.Lock()
		if entry.conn != nil {
			_ = entry.conn.Close()
			entry.conn = nil
		}
		entry.mu.Unlock()
	}
}
func (c *Client) entry(imei string) *deviceConn {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.conns == nil {
		c.conns = map[string]*deviceConn{}
	}
	entry := c.conns[imei]
	if entry == nil {
		entry = &deviceConn{}
		c.conns[imei] = entry
	}
	return entry
}
func (c *Client) Send(ctx context.Context, t Telemetry) error {
	if c.Address == "" {
		return nil
	}
	entry := c.entry(t.IMEI)
	entry.mu.Lock()
	defer entry.mu.Unlock()
	if entry.conn == nil {
		conn, err := c.connect(ctx, t.IMEI)
		if err != nil {
			return err
		}
		entry.conn = conn
	}
	if err := c.send(entry.conn, t); err != nil {
		_ = entry.conn.Close()
		entry.conn = nil
		conn, reconnectErr := c.connect(ctx, t.IMEI)
		if reconnectErr != nil {
			return fmt.Errorf("send failed: %v; reconnect failed: %w", err, reconnectErr)
		}
		entry.conn = conn
		return c.send(entry.conn, t)
	}
	return nil
}
func (c *Client) connect(ctx context.Context, imei string) (net.Conn, error) {
	timeout := c.Timeout
	if timeout <= 0 {
		timeout = 5 * time.Second
	}
	conn, err := (&net.Dialer{Timeout: timeout}).DialContext(ctx, "tcp", c.Address)
	if err != nil {
		return nil, fmt.Errorf("connect Teltonika gateway: %w", err)
	}
	_ = conn.SetDeadline(time.Now().Add(timeout))
	identity, err := EncodeIMEI(imei)
	if err != nil {
		_ = conn.Close()
		return nil, err
	}
	if _, err = conn.Write(identity); err != nil {
		_ = conn.Close()
		return nil, fmt.Errorf("send IMEI: %w", err)
	}
	var accepted [1]byte
	if _, err = io.ReadFull(conn, accepted[:]); err != nil {
		_ = conn.Close()
		return nil, fmt.Errorf("read IMEI acknowledgement: %w", err)
	}
	if accepted[0] != 1 {
		_ = conn.Close()
		return nil, fmt.Errorf("gateway rejected IMEI")
	}
	_ = conn.SetDeadline(time.Time{})
	return conn, nil
}
func (c *Client) send(conn net.Conn, t Telemetry) error {
	timeout := c.Timeout
	if timeout <= 0 {
		timeout = 5 * time.Second
	}
	_ = conn.SetDeadline(time.Now().Add(timeout))
	r := Record{Timestamp: t.Timestamp, Latitude: t.Latitude, Longitude: t.Longitude, Speed: t.Speed, Heading: t.Heading, Ignition: t.Ignition}
	var packet []byte
	var err error
	if strings.EqualFold(c.Codec, "8") {
		packet, err = EncodeCodec8(r)
	} else {
		packet, err = EncodeCodec8Extended(r)
	}
	if err != nil {
		return err
	}
	if _, err = conn.Write(packet); err != nil {
		return fmt.Errorf("send AVL packet: %w", err)
	}
	var ack [4]byte
	if _, err = io.ReadFull(conn, ack[:]); err != nil {
		return fmt.Errorf("read AVL acknowledgement: %w", err)
	}
	if n := binary.BigEndian.Uint32(ack[:]); n != 1 {
		return fmt.Errorf("gateway acknowledged %d AVL records, want 1", n)
	}
	_ = conn.SetDeadline(time.Time{})
	return nil
}
