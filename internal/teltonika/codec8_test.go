package teltonika

import (
	"testing"
	"time"
)

func TestCodec8Packet(t *testing.T) {
	p, err := EncodeCodec8(Record{Timestamp: time.Unix(1, 0), Latitude: -2.98, Longitude: 104.78, Speed: 40, Heading: 90, Ignition: true})
	if err != nil {
		t.Fatal(err)
	}
	if len(p) < 20 || p[8] != 0x08 {
		t.Fatalf("invalid Codec 8 packet")
	}
}
