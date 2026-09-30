package teltonika

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net"
	"strings"
	"sync"
	"time"
)

// MaxRecords is the most records the client puts in one packet; Sensor's default packet limit
// is 16 KiB.
const MaxRecords = 50

// ErrRefused is returned while the gateway refuses a device's IMEI, as Sensor does for a device
// that is not registered yet.
var ErrRefused = errors.New("gateway refused the IMEI (not registered?)")

// refusalBackoff is how long a refused IMEI waits before it tries again. Sensor's registry
// cache takes up to 30 s to see a new registration.
const refusalBackoff = 20 * time.Second

// Client keeps one persistent TCP session per IMEI, as each tracker does.
type Client struct {
	Address string
	Timeout time.Duration
	Codec   string // "8" or "8E" (default)
	mu      sync.Mutex
	conns   map[string]*deviceConn
}

type deviceConn struct {
	mu           sync.Mutex
	conn         net.Conn
	refusedUntil time.Time
}

// Close ends every session.
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

func (c *Client) codec() byte {
	if strings.EqualFold(strings.TrimSpace(c.Codec), "8") {
		return Codec8
	}
	return Codec8E
}

// Send delivers a device's records in packets of at most MaxRecords, each acknowledged with its
// record count before the next. A broken session is reopened once.
func (c *Client) Send(ctx context.Context, imei string, records []Record) error {
	if c.Address == "" || len(records) == 0 {
		return nil
	}
	entry := c.entry(imei)
	entry.mu.Lock()
	defer entry.mu.Unlock()
	if time.Now().Before(entry.refusedUntil) {
		return ErrRefused
	}
	for start := 0; start < len(records); start += MaxRecords {
		chunk := records[start:min(len(records), start+MaxRecords)]
		packet, err := Encode(c.codec(), chunk)
		if err != nil {
			return err
		}
		if err := c.deliver(ctx, entry, imei, packet, len(chunk)); err != nil {
			return err
		}
	}
	return nil
}

func (c *Client) deliver(ctx context.Context, entry *deviceConn, imei string, packet []byte, count int) error {
	reused := entry.conn != nil
	if entry.conn == nil {
		conn, err := c.connect(ctx, imei)
		if err != nil {
			if errors.Is(err, ErrRefused) {
				entry.refusedUntil = time.Now().Add(refusalBackoff)
			}
			return err
		}
		entry.conn = conn
	}
	err := c.send(entry.conn, packet, count)
	if err == nil {
		return nil
	}
	_ = entry.conn.Close()
	entry.conn = nil
	if !reused {
		return err
	}
	conn, reconnectErr := c.connect(ctx, imei)
	if reconnectErr != nil {
		if errors.Is(reconnectErr, ErrRefused) {
			entry.refusedUntil = time.Now().Add(refusalBackoff)
		}
		return fmt.Errorf("send failed: %v; reconnect failed: %w", err, reconnectErr)
	}
	entry.conn = conn
	if err := c.send(conn, packet, count); err != nil {
		_ = conn.Close()
		entry.conn = nil
		return err
	}
	return nil
}

func (c *Client) timeout() time.Duration {
	if c.Timeout <= 0 {
		return 5 * time.Second
	}
	return c.Timeout
}

func (c *Client) connect(ctx context.Context, imei string) (net.Conn, error) {
	conn, err := (&net.Dialer{Timeout: c.timeout()}).DialContext(ctx, "tcp", c.Address)
	if err != nil {
		return nil, fmt.Errorf("connect Teltonika gateway: %w", err)
	}
	_ = conn.SetDeadline(time.Now().Add(c.timeout()))
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
		if errors.Is(err, io.EOF) {
			return nil, ErrRefused
		}
		return nil, fmt.Errorf("read IMEI acknowledgement: %w", err)
	}
	if accepted[0] != 1 {
		_ = conn.Close()
		return nil, ErrRefused
	}
	_ = conn.SetDeadline(time.Time{})
	return conn, nil
}

func (c *Client) send(conn net.Conn, packet []byte, count int) error {
	_ = conn.SetDeadline(time.Now().Add(c.timeout()))
	defer conn.SetDeadline(time.Time{})
	if _, err := conn.Write(packet); err != nil {
		return fmt.Errorf("send AVL packet: %w", err)
	}
	var ack [4]byte
	if _, err := io.ReadFull(conn, ack[:]); err != nil {
		return fmt.Errorf("read AVL acknowledgement: %w", err)
	}
	if n := binary.BigEndian.Uint32(ack[:]); int(n) != count {
		return fmt.Errorf("gateway acknowledged %d AVL records, want %d", n, count)
	}
	return nil
}
