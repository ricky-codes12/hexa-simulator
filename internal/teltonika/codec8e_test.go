package teltonika

import (
	"encoding/binary"
	"encoding/hex"
	"testing"
	"time"
)

func TestEncodeIMEI(t *testing.T) {
	got, err := EncodeIMEI("352093081234567")
	if err != nil {
		t.Fatal(err)
	}
	if binary.BigEndian.Uint16(got[:2]) != 15 || string(got[2:]) != "352093081234567" {
		t.Fatalf("unexpected IMEI frame: %x", got)
	}
	for _, invalid := range []string{"123", "35209308123456x"} {
		if _, err := EncodeIMEI(invalid); err == nil {
			t.Fatalf("expected invalid IMEI %q to fail", invalid)
		}
	}
}

func TestEncodeCodec8ExtendedPacketShapeAndCRC(t *testing.T) {
	packet, err := EncodeCodec8Extended(Record{Timestamp: time.UnixMilli(1560161086000).UTC(), Latitude: -6.2088, Longitude: 106.8456, Speed: 42, Heading: 90, Ignition: true})
	if err != nil {
		t.Fatal(err)
	}
	if len(packet) < 13 || packet[0] != 0 || packet[1] != 0 || packet[2] != 0 || packet[3] != 0 {
		t.Fatalf("missing preamble: %x", packet)
	}
	dataLen := int(binary.BigEndian.Uint32(packet[4:8]))
	if dataLen != len(packet)-12 {
		t.Fatalf("data length=%d packet=%d", dataLen, len(packet))
	}
	data := packet[8 : 8+dataLen]
	if data[0] != 0x8e || data[1] != 1 || data[len(data)-1] != 1 {
		t.Fatalf("invalid Codec8E envelope: %x", data)
	}
	gotCRC := binary.BigEndian.Uint32(packet[len(packet)-4:])
	if gotCRC != uint32(crc16IBM(data)) {
		t.Fatalf("crc=%08x want=%04x", gotCRC, crc16IBM(data))
	}
}

func TestCRC16IBMMatchesTeltonikaCodec8Example(t *testing.T) {
	data, err := hex.DecodeString("08010000016B40D8EA30010000000000000000000000000000000105021503010101425E0F01F10000601A014E000000000000000001")
	if err != nil {
		t.Fatal(err)
	}
	if got := crc16IBM(data); got != 0xC7CF {
		t.Fatalf("crc=%04X want=C7CF", got)
	}
}
