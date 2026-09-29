package mqttout

import (
	"bufio"
	"context"
	"encoding/binary"
	"io"
	"net"
	"strings"
	"testing"
	"time"
)

func TestPublishQoS1(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	done := make(chan struct{})
	go func() {
		defer close(done)
		c, _ := ln.Accept()
		defer c.Close()
		r := bufio.NewReader(c)
		_, _ = readTestPacket(r)
		_, _ = c.Write([]byte{0x20, 0x02, 0, 0})
		h, b := readTestPacket(r)
		if h != 0x32 {
			t.Errorf("header=%x", h)
			return
		}
		topicLen := int(binary.BigEndian.Uint16(b[:2]))
		off := 2 + topicLen
		id := binary.BigEndian.Uint16(b[off : off+2])
		payload := string(b[off+2:])
		for _, want := range []string{`"schema":"hexa.sensor/telemetry/v1"`, `"device":{"hardware_id":"352093081234567"}`, `"fix_valid":true`, `"lat":-2.9`, `"lon":104.8`, `"speed_kmh":40`, `"heading_deg":90`} {
			if !strings.Contains(payload, want) {
				t.Errorf("payload missing %s: %s", want, payload)
			}
		}
		ack := []byte{0x40, 0x02, byte(id >> 8), byte(id)}
		_, _ = c.Write(ack)
	}()
	cl := &Client{URL: "mqtt://" + ln.Addr().String(), Topic: "{imei}/data", Timeout: time.Second}
	defer cl.Close()
	if err := cl.Publish(context.Background(), Telemetry{HardwareID: "352093081234567", DeviceTime: time.Now(), Latitude: -2.9, Longitude: 104.8, Speed: 40, Heading: 90, Ignition: true, Movement: true}); err != nil {
		t.Fatal(err)
	}
	<-done
}
func readTestPacket(r *bufio.Reader) (byte, []byte) {
	h, _ := r.ReadByte()
	n := 0
	m := 1
	for {
		b, _ := r.ReadByte()
		n += int(b&127) * m
		if b&128 == 0 {
			break
		}
		m *= 128
	}
	body := make([]byte, n)
	_, _ = io.ReadFull(r, body)
	return h, body
}
