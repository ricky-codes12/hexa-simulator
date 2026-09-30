package teltonika

import (
	"context"
	"encoding/binary"
	"errors"
	"io"
	"net"
	"testing"
	"time"
)

// gateway accepts connections, answers the handshake with accept, and acknowledges each packet
// with its record count, reporting the counts on got.
func gateway(t *testing.T, accept byte, got chan<- int) string {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ln.Close() })
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			go func(conn net.Conn) {
				defer conn.Close()
				var length [2]byte
				if _, err := io.ReadFull(conn, length[:]); err != nil {
					return
				}
				imei := make([]byte, binary.BigEndian.Uint16(length[:]))
				if _, err := io.ReadFull(conn, imei); err != nil {
					return
				}
				_, _ = conn.Write([]byte{accept})
				if accept != 1 {
					return
				}
				for {
					var header [8]byte
					if _, err := io.ReadFull(conn, header[:]); err != nil {
						return
					}
					rest := make([]byte, int(binary.BigEndian.Uint32(header[4:]))+4)
					if _, err := io.ReadFull(conn, rest); err != nil {
						return
					}
					_, records, err := Decode(append(header[:], rest...))
					if err != nil {
						t.Errorf("decode: %v", err)
						return
					}
					got <- len(records)
					_, _ = conn.Write(binary.BigEndian.AppendUint32(nil, uint32(len(records))))
				}
			}(conn)
		}
	}()
	return ln.Addr().String()
}

func TestClientSendsBurstsInAcknowledgedPackets(t *testing.T) {
	got := make(chan int, 8)
	c := &Client{Address: gateway(t, 1, got), Timeout: time.Second}
	defer c.Close()
	records := make([]Record, MaxRecords+7)
	for i := range records {
		records[i] = Record{Timestamp: time.Now().Add(time.Duration(i) * time.Second), Latitude: -2.02, Longitude: 104.06, Satellites: 10}
	}
	if err := c.Send(context.Background(), "359815020000017", records); err != nil {
		t.Fatal(err)
	}
	if a, b := <-got, <-got; a != MaxRecords || b != 7 {
		t.Fatalf("packets of %d and %d records", a, b)
	}
	// The session stays open for the next record.
	if err := c.Send(context.Background(), "359815020000017", records[:1]); err != nil {
		t.Fatal(err)
	}
	if n := <-got; n != 1 {
		t.Fatalf("packet of %d", n)
	}
}

func TestRefusedIMEIBacksOff(t *testing.T) {
	c := &Client{Address: gateway(t, 0, make(chan int, 1)), Timeout: time.Second}
	defer c.Close()
	r := []Record{{Timestamp: time.Now(), Latitude: -2.02, Longitude: 104.06}}
	if err := c.Send(context.Background(), "359815020000017", r); !errors.Is(err, ErrRefused) {
		t.Fatalf("err=%v", err)
	}
	start := time.Now()
	if err := c.Send(context.Background(), "359815020000017", r); !errors.Is(err, ErrRefused) || time.Since(start) > 50*time.Millisecond {
		t.Fatalf("second send err=%v after %v; want an immediate refusal while backing off", err, time.Since(start))
	}
}
