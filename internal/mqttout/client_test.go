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

	"hexa-simulator/internal/telemetry"
)

func record() telemetry.Record {
	return telemetry.Record{
		HardwareID: "359815010000121", Time: time.Date(2026, 9, 30, 8, 0, 0, 0, time.UTC),
		Position:   &telemetry.Position{Lat: -2.02, Lon: 104.05, SpeedKmh: 40, HeadingDeg: 90, Satellites: 11, FixValid: true},
		Attributes: map[string]any{"ignition": true, "movement": true, "external_voltage": 27.6},
	}
}

// broker accepts one connection, checks CONNECT with check, answers CONNACK with code and then
// acknowledges every PUBLISH, handing the topic and payload to got.
func broker(t *testing.T, code byte, check func(flags byte, payload []byte), got chan<- [2]string) string {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ln.Close() })
	go func() {
		c, err := ln.Accept()
		if err != nil {
			return
		}
		defer c.Close()
		r := bufio.NewReader(c)
		h, b := readTestPacket(r)
		if h != 0x10 {
			t.Errorf("first packet %x, want CONNECT", h)
			return
		}
		check(b[7], b[10:])
		_, _ = c.Write([]byte{0x20, 0x02, 0, code})
		for {
			h, b := readTestPacket(r)
			if h != 0x32 {
				return
			}
			n := int(binary.BigEndian.Uint16(b[:2]))
			id := binary.BigEndian.Uint16(b[2+n : 4+n])
			got <- [2]string{string(b[2 : 2+n]), string(b[4+n:])}
			_, _ = c.Write([]byte{0x40, 0x02, byte(id >> 8), byte(id)})
		}
	}()
	return "mqtt://" + ln.Addr().String()
}

func TestPublishesTelemetryV1WithCredentials(t *testing.T) {
	got := make(chan [2]string, 2)
	url := broker(t, 0, func(flags byte, payload []byte) {
		if flags&0xC0 != 0xC0 {
			t.Errorf("CONNECT flags %08b: want username and password", flags)
		}
		for _, want := range []string{"sim-test", "hexa-simulator", "s3cret"} {
			if !strings.Contains(string(payload), want) {
				t.Errorf("CONNECT payload lacks %q", want)
			}
		}
	}, got)
	c := &Client{URL: url, Topic: "{imei}/data", ClientID: "sim-test", Username: "hexa-simulator", Password: "s3cret", Timeout: time.Second}
	defer c.Close()
	if err := c.PublishRecords(context.Background(), []telemetry.Record{record()}, Topics{Kind: "haul-truck"}); err != nil {
		t.Fatal(err)
	}
	msg := <-got
	if msg[0] != "359815010000121/data" {
		t.Fatalf("topic %q", msg[0])
	}
	for _, want := range []string{`"schema":"hexa.sensor/telemetry/v1"`, `"device":{"hardware_id":"359815010000121"}`, `"device_time":"2026-09-30T08:00:00.000Z"`,
		`"fix_valid":true`, `"speed_kmh":40`, `"heading_deg":90`, `"external_voltage":27.6`} {
		if !strings.Contains(msg[1], want) {
			t.Errorf("payload lacks %s: %s", want, msg[1])
		}
	}
	// A burst goes out as one {"records": [...]} message.
	if err := c.PublishRecords(context.Background(), []telemetry.Record{record(), record()}, Topics{}); err != nil {
		t.Fatal(err)
	}
	if msg := <-got; !strings.HasPrefix(msg[1], `{"records":[{`) {
		t.Fatalf("burst payload %s", msg[1])
	}
}

func TestRefusedPasswordIsNamed(t *testing.T) {
	url := broker(t, 4, func(byte, []byte) {}, make(chan [2]string, 1))
	c := &Client{URL: url, Username: "hexa-simulator", Password: "wrong", Timeout: time.Second}
	defer c.Close()
	err := c.PublishRecords(context.Background(), []telemetry.Record{record()}, Topics{})
	if err == nil || !strings.Contains(err.Error(), "username or password") {
		t.Fatalf("err=%v", err)
	}
}

func TestCredentialsFromURL(t *testing.T) {
	got := make(chan [2]string, 1)
	url := broker(t, 0, func(flags byte, payload []byte) {
		if flags&0xC0 != 0xC0 || !strings.Contains(string(payload), "pw") {
			t.Errorf("flags %08b payload %q", flags, payload)
		}
	}, got)
	c := &Client{URL: strings.Replace(url, "mqtt://", "mqtt://user:pw@", 1), Timeout: time.Second}
	defer c.Close()
	if err := c.PublishRecords(context.Background(), []telemetry.Record{record()}, Topics{}); err != nil {
		t.Fatal(err)
	}
	<-got
}

func readTestPacket(r *bufio.Reader) (byte, []byte) {
	h, err := r.ReadByte()
	if err != nil {
		return 0, nil
	}
	n, m := 0, 1
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
