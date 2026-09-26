package teltonika

import (
	"testing"
	"time"
)

func TestDecodeCodec8ExtendedRoundTrip(t *testing.T) {
	want := Record{Timestamp: time.Date(2026, 9, 26, 12, 0, 0, 123000000, time.UTC), Latitude: -2.985, Longitude: 104.767, Speed: 42, Heading: 180, Ignition: true}
	packet, err := EncodeCodec8Extended(want)
	if err != nil {
		t.Fatal(err)
	}
	got, err := DecodeCodec8Extended(packet)
	if err != nil {
		t.Fatal(err)
	}
	if !got.Timestamp.Equal(want.Timestamp) || got.Latitude != want.Latitude || got.Longitude != want.Longitude || got.Speed != want.Speed || got.Heading != want.Heading || got.Ignition != want.Ignition {
		t.Fatalf("decoded record = %+v, want %+v", got, want)
	}
}

func TestDecodeCodec8ExtendedRejectsBadCRC(t *testing.T) {
	packet, err := EncodeCodec8Extended(Record{Timestamp: time.Now(), Latitude: -6.2, Longitude: 106.8})
	if err != nil {
		t.Fatal(err)
	}
	packet[len(packet)-1] ^= 0xff
	if _, err := DecodeCodec8Extended(packet); err == nil {
		t.Fatal("expected CRC error")
	}
}
