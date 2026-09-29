package mqttout

import (
	"bufio"
	"context"
	"crypto/tls"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/url"
	"strings"
	"sync"
	"time"
)

type Telemetry struct {
	HardwareID                          string
	DeviceTime                          time.Time
	Latitude, Longitude, Speed, Heading float64
	Ignition, Movement                  bool
}
type Client struct {
	URL, Topic string
	Timeout    time.Duration
	mu         sync.Mutex
	conn       net.Conn
	reader     *bufio.Reader
	packetID   uint16
}

func (c *Client) Close() {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.conn != nil {
		_ = c.conn.Close()
		c.conn = nil
	}
}
func (c *Client) Publish(ctx context.Context, t Telemetry) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if err := c.ensure(ctx); err != nil {
		return err
	}
	payload, _ := json.Marshal(map[string]any{"schema": "hexa.sensor/telemetry/v1", "hardware_id": t.HardwareID, "device_time": t.DeviceTime.UTC().Format(time.RFC3339Nano), "position": map[string]any{"latitude": t.Latitude, "longitude": t.Longitude, "speed": t.Speed, "heading": t.Heading}, "attributes": map[string]any{"ignition": t.Ignition, "movement": t.Movement}})
	topic := strings.ReplaceAll(c.Topic, "{imei}", t.HardwareID)
	c.packetID++
	if c.packetID == 0 {
		c.packetID = 1
	}
	body := appendString(nil, topic)
	var id [2]byte
	binary.BigEndian.PutUint16(id[:], c.packetID)
	body = append(body, id[:]...)
	body = append(body, payload...)
	if err := c.writePacket(0x32, body); err != nil {
		c.drop()
		return err
	}
	h, body, err := c.readPacket()
	if err != nil {
		c.drop()
		return err
	}
	if h>>4 != 4 || len(body) != 2 || binary.BigEndian.Uint16(body) != c.packetID {
		return fmt.Errorf("mqtt PUBACK mismatch")
	}
	return nil
}
func (c *Client) ensure(ctx context.Context) error {
	if c.conn != nil {
		return nil
	}
	u, err := url.Parse(c.URL)
	if err != nil {
		return err
	}
	timeout := c.Timeout
	if timeout <= 0 {
		timeout = 5 * time.Second
	}
	d := net.Dialer{Timeout: timeout}
	var conn net.Conn
	if u.Scheme == "mqtts" {
		conn, err = tls.DialWithDialer(&d, "tcp", u.Host, &tls.Config{ServerName: u.Hostname(), MinVersion: tls.VersionTLS12})
	} else if u.Scheme == "mqtt" {
		conn, err = d.DialContext(ctx, "tcp", u.Host)
	} else {
		return fmt.Errorf("mqtt URL must use mqtt:// or mqtts://")
	}
	if err != nil {
		return fmt.Errorf("mqtt connect: %w", err)
	}
	c.conn = conn
	c.reader = bufio.NewReader(conn)
	_ = conn.SetDeadline(time.Now().Add(timeout))
	flags := byte(2)
	payload := appendString(nil, "hexa-simulator")
	vh := []byte{0, 4, 'M', 'Q', 'T', 'T', 4, flags, 0, 30}
	if err := c.writePacket(0x10, append(vh, payload...)); err != nil {
		c.drop()
		return err
	}
	h, b, err := c.readPacket()
	if err != nil {
		c.drop()
		return err
	}
	if h>>4 != 2 || len(b) < 2 || b[1] != 0 {
		c.drop()
		return fmt.Errorf("mqtt broker rejected connection")
	}
	_ = conn.SetDeadline(time.Time{})
	return nil
}
func (c *Client) drop() {
	if c.conn != nil {
		_ = c.conn.Close()
	}
	c.conn = nil
	c.reader = nil
}
func (c *Client) writePacket(header byte, body []byte) error {
	packet := []byte{header}
	n := len(body)
	for {
		digit := byte(n % 128)
		n /= 128
		if n > 0 {
			digit |= 128
		}
		packet = append(packet, digit)
		if n == 0 {
			break
		}
	}
	packet = append(packet, body...)
	_, err := c.conn.Write(packet)
	return err
}
func (c *Client) readPacket() (byte, []byte, error) {
	h, err := c.reader.ReadByte()
	if err != nil {
		return 0, nil, err
	}
	mult, n := 1, 0
	for i := 0; i < 4; i++ {
		b, e := c.reader.ReadByte()
		if e != nil {
			return 0, nil, e
		}
		n += int(b&127) * mult
		if b&128 == 0 {
			break
		}
		mult *= 128
	}
	body := make([]byte, n)
	_, err = io.ReadFull(c.reader, body)
	return h, body, err
}
func appendString(dst []byte, s string) []byte {
	var b [2]byte
	binary.BigEndian.PutUint16(b[:], uint16(len(s)))
	dst = append(dst, b[:]...)
	return append(dst, []byte(s)...)
}
