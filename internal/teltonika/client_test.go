package teltonika

import (
	"context"
	"encoding/binary"
	"io"
	"net"
	"testing"
	"time"
)

func TestClientPerformsTeltonikaHandshakeAndAVLACK(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	serverErr := make(chan error, 1)
	go func() {
		conn, err := listener.Accept()
		if err != nil {
			serverErr <- err
			return
		}
		defer conn.Close()
		var length [2]byte
		if _, err := io.ReadFull(conn, length[:]); err != nil {
			serverErr <- err
			return
		}
		imei := make([]byte, binary.BigEndian.Uint16(length[:]))
		if _, err := io.ReadFull(conn, imei); err != nil {
			serverErr <- err
			return
		}
		if string(imei) != "352093081234567" {
			serverErr <- &testError{"unexpected IMEI"}
			return
		}
		if _, err := conn.Write([]byte{1}); err != nil {
			serverErr <- err
			return
		}
		var header [8]byte
		if _, err := io.ReadFull(conn, header[:]); err != nil {
			serverErr <- err
			return
		}
		payload := make([]byte, int(binary.BigEndian.Uint32(header[4:]))+4)
		if _, err := io.ReadFull(conn, payload); err != nil {
			serverErr <- err
			return
		}
		if payload[0] != 0x8e {
			serverErr <- &testError{"not Codec8E"}
			return
		}
		_, err = conn.Write([]byte{0, 0, 0, 1})
		serverErr <- err
	}()
	client := Client{Address: listener.Addr().String(), Timeout: time.Second}
	err = client.Send(context.Background(), Telemetry{IMEI: "352093081234567", Timestamp: time.Now(), Latitude: -6.2, Longitude: 106.8, Speed: 42, Heading: 90, Ignition: true})
	if err != nil {
		t.Fatal(err)
	}
	if err := <-serverErr; err != nil {
		t.Fatal(err)
	}
}

type testError struct{ message string }

func (e *testError) Error() string { return e.message }
