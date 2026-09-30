// Package mqttout publishes telemetry-v1 records to an MQTT 3.1.1 broker at QoS 1, as a
// customer's gateway or vendor cloud would, for Hexa.Sensor's mqtt-subscribe source.
package mqttout

import (
	"bufio"
	"context"
	"crypto/tls"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/url"
	"strings"
	"sync"
	"time"

	"hexa-simulator/internal/telemetry"
)

// Client keeps one broker connection and publishes each message at QoS 1, waiting for its
// PUBACK. It reconnects on the next publish after a failure.
type Client struct {
	URL string
	// Topic is a template: {imei} (or {hardware_id}), {kind} and {estate} are replaced per record.
	Topic     string
	ClientID  string
	Username  string
	Password  string
	Timeout   time.Duration
	KeepAlive time.Duration

	mu       sync.Mutex
	conn     net.Conn
	reader   *bufio.Reader
	packetID uint16
	lastSent time.Time
	stop     chan struct{}
}

// Topics the template knows.
type Topics struct{ Kind, Estate string }

// PublishRecords publishes a device's records as one message: a single record as a telemetry-v1
// object, several (a burst) as {"records": [...]}, which Sensor's mqtt-subscribe accepts too.
func (c *Client) PublishRecords(ctx context.Context, records []telemetry.Record, t Topics) error {
	if len(records) == 0 {
		return nil
	}
	var payload []byte
	var err error
	if len(records) == 1 {
		payload, err = json.Marshal(records[0])
	} else {
		payload, err = json.Marshal(map[string]any{"records": records})
	}
	if err != nil {
		return err
	}
	return c.Publish(ctx, c.topic(records[0].HardwareID, t), payload)
}

func (c *Client) topic(hardwareID string, t Topics) string {
	topic := c.Topic
	if topic == "" {
		topic = "{imei}/data"
	}
	return strings.NewReplacer("{imei}", hardwareID, "{hardware_id}", hardwareID, "{kind}", t.Kind, "{estate}", t.Estate).Replace(topic)
}

// Publish sends one message at QoS 1. A message that fails on a connection that was already
// open is retried once on a fresh connection; Sensor deduplicates a resent record.
func (c *Client) Publish(ctx context.Context, topic string, payload []byte) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	reused := c.conn != nil
	err := c.publish(ctx, topic, payload)
	if err != nil && reused && !isRefusal(err) {
		c.drop()
		err = c.publish(ctx, topic, payload)
	}
	return err
}

func (c *Client) publish(ctx context.Context, topic string, payload []byte) error {
	if err := c.ensure(ctx); err != nil {
		return err
	}
	_ = c.conn.SetDeadline(time.Now().Add(c.timeout()))
	defer func() {
		if c.conn != nil {
			_ = c.conn.SetDeadline(time.Time{})
		}
	}()
	c.packetID++
	if c.packetID == 0 {
		c.packetID = 1
	}
	body := appendString(nil, topic)
	body = binary.BigEndian.AppendUint16(body, c.packetID)
	body = append(body, payload...)
	if err := c.writePacket(0x32, body); err != nil {
		c.drop()
		return fmt.Errorf("mqtt publish: %w", err)
	}
	for {
		h, b, err := c.readPacket()
		if err != nil {
			c.drop()
			return fmt.Errorf("mqtt PUBACK: %w", err)
		}
		switch h >> 4 {
		case 13: // PINGRESP from an earlier keep-alive
			continue
		case 4:
			if len(b) != 2 || binary.BigEndian.Uint16(b) != c.packetID {
				c.drop()
				return fmt.Errorf("mqtt PUBACK mismatch")
			}
			return nil
		default:
			c.drop()
			return fmt.Errorf("mqtt: unexpected packet type %d while waiting for PUBACK", h>>4)
		}
	}
}

// Close disconnects.
func (c *Client) Close() {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.conn != nil {
		_ = c.writePacket(0xE0, nil)
	}
	c.drop()
}

func (c *Client) timeout() time.Duration {
	if c.Timeout <= 0 {
		return 5 * time.Second
	}
	return c.Timeout
}

func (c *Client) keepAlive() time.Duration {
	if c.KeepAlive <= 0 {
		return 30 * time.Second
	}
	return c.KeepAlive
}

type refusal struct{ msg string }

func (r refusal) Error() string { return r.msg }

func isRefusal(err error) bool {
	var r refusal
	return errors.As(err, &r)
}

func (c *Client) ensure(ctx context.Context) error {
	if c.conn != nil {
		return nil
	}
	u, err := url.Parse(c.URL)
	if err != nil {
		return err
	}
	d := net.Dialer{Timeout: c.timeout()}
	var conn net.Conn
	switch u.Scheme {
	case "mqtts":
		conn, err = (&tls.Dialer{NetDialer: &d, Config: &tls.Config{ServerName: u.Hostname(), MinVersion: tls.VersionTLS12}}).DialContext(ctx, "tcp", u.Host)
	case "mqtt":
		conn, err = d.DialContext(ctx, "tcp", u.Host)
	default:
		return refusal{"mqtt URL must use mqtt:// or mqtts://"}
	}
	if err != nil {
		return fmt.Errorf("mqtt connect: %w", err)
	}
	c.conn = conn
	c.reader = bufio.NewReader(conn)
	_ = conn.SetDeadline(time.Now().Add(c.timeout()))
	username, password := c.Username, c.Password
	if u.User != nil {
		if username == "" {
			username = u.User.Username()
		}
		if p, ok := u.User.Password(); ok && password == "" {
			password = p
		}
	}
	clientID := c.ClientID
	if clientID == "" {
		clientID = "hexa-simulator"
	}
	flags := byte(0x02) // clean session: the simulator only publishes
	payload := appendString(nil, clientID)
	if username != "" {
		flags |= 0x80
		payload = appendString(payload, username)
		if password != "" {
			flags |= 0x40
			payload = appendString(payload, password)
		}
	}
	keep := uint16(c.keepAlive() / time.Second)
	vh := []byte{0, 4, 'M', 'Q', 'T', 'T', 4, flags}
	vh = binary.BigEndian.AppendUint16(vh, keep)
	if err := c.writePacket(0x10, append(vh, payload...)); err != nil {
		c.drop()
		return fmt.Errorf("mqtt connect: %w", err)
	}
	h, b, err := c.readPacket()
	if err != nil {
		c.drop()
		return fmt.Errorf("mqtt CONNACK: %w", err)
	}
	if h>>4 != 2 || len(b) < 2 {
		c.drop()
		return fmt.Errorf("mqtt broker answered CONNECT with packet type %d", h>>4)
	}
	if b[1] != 0 {
		c.drop()
		return refusal{connackReason(b[1])}
	}
	_ = conn.SetDeadline(time.Time{})
	c.stop = make(chan struct{})
	go c.pinger(conn, c.stop)
	return nil
}

func connackReason(code byte) string {
	switch code {
	case 1:
		return "mqtt broker refused the protocol version"
	case 2:
		return "mqtt broker refused the client ID"
	case 3:
		return "mqtt broker is unavailable"
	case 4:
		return "mqtt broker refused the username or password"
	case 5:
		return "mqtt broker says this client is not authorised"
	}
	return fmt.Sprintf("mqtt broker refused the connection (code %d)", code)
}

// pinger keeps an idle connection alive: when nothing was sent for half the keep-alive, it
// sends PINGREQ. The PINGRESP is read by the next publish, or here when idle.
func (c *Client) pinger(conn net.Conn, stop chan struct{}) {
	t := time.NewTicker(c.keepAlive() / 2)
	defer t.Stop()
	for {
		select {
		case <-stop:
			return
		case <-t.C:
		}
		c.mu.Lock()
		if c.conn != conn {
			c.mu.Unlock()
			return
		}
		if time.Since(c.lastSent) >= c.keepAlive()/2 {
			_ = conn.SetDeadline(time.Now().Add(c.timeout()))
			if err := c.writePacket(0xC0, nil); err != nil {
				c.drop()
			} else if h, _, err := c.readPacket(); err != nil || h>>4 != 13 {
				c.drop()
			} else {
				_ = conn.SetDeadline(time.Time{})
			}
		}
		c.mu.Unlock()
	}
}

func (c *Client) drop() {
	if c.stop != nil {
		close(c.stop)
		c.stop = nil
	}
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
	if err == nil {
		c.lastSent = time.Now()
	}
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
	dst = binary.BigEndian.AppendUint16(dst, uint16(len(s)))
	return append(dst, s...)
}
