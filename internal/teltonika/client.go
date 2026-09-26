package teltonika

import (
	"context"
	"encoding/binary"
	"fmt"
	"io"
	"net"
	"time"
)

type Client struct {
	Address string
	Timeout time.Duration
}

type Telemetry struct {
	IMEI      string
	Timestamp time.Time
	Latitude  float64
	Longitude float64
	Speed     float64
	Heading   float64
	Ignition  bool
}

func (c Client) Send(ctx context.Context, telemetry Telemetry) error {
	if c.Address == "" {
		return nil
	}
	timeout := c.Timeout
	if timeout <= 0 {
		timeout = 5 * time.Second
	}
	dialer := net.Dialer{Timeout: timeout}
	conn, err := dialer.DialContext(ctx, "tcp", c.Address)
	if err != nil {
		return fmt.Errorf("connect Teltonika gateway: %w", err)
	}
	defer conn.Close()
	deadline := time.Now().Add(timeout)
	if d, ok := ctx.Deadline(); ok && d.Before(deadline) {
		deadline = d
	}
	if err := conn.SetDeadline(deadline); err != nil {
		return fmt.Errorf("set gateway deadline: %w", err)
	}

	identity, err := EncodeIMEI(telemetry.IMEI)
	if err != nil {
		return err
	}
	if _, err := conn.Write(identity); err != nil {
		return fmt.Errorf("send IMEI: %w", err)
	}
	var accepted [1]byte
	if _, err := io.ReadFull(conn, accepted[:]); err != nil {
		return fmt.Errorf("read IMEI acknowledgement: %w", err)
	}
	if accepted[0] != 0x01 {
		return fmt.Errorf("gateway rejected IMEI")
	}

	packet, err := EncodeCodec8Extended(Record{Timestamp: telemetry.Timestamp, Latitude: telemetry.Latitude, Longitude: telemetry.Longitude, Speed: telemetry.Speed, Heading: telemetry.Heading, Ignition: telemetry.Ignition})
	if err != nil {
		return err
	}
	if _, err := conn.Write(packet); err != nil {
		return fmt.Errorf("send AVL packet: %w", err)
	}
	var ack [4]byte
	if _, err := io.ReadFull(conn, ack[:]); err != nil {
		return fmt.Errorf("read AVL acknowledgement: %w", err)
	}
	if count := binary.BigEndian.Uint32(ack[:]); count != 1 {
		return fmt.Errorf("gateway acknowledged %d AVL records, want 1", count)
	}
	return nil
}
