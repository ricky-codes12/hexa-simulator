package teltonika

import (
	"encoding/binary"
	"encoding/hex"
	"testing"
	"time"

	"hexa-simulator/internal/telemetry"
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

func TestCRC16IBMMatchesTeltonikaCodec8Example(t *testing.T) {
	data, err := hex.DecodeString("08010000016B40D8EA30010000000000000000000000000000000105021503010101425E0F01F10000601A014E000000000000000001")
	if err != nil {
		t.Fatal(err)
	}
	if got := crc16IBM(data); got != 0xC7CF {
		t.Fatalf("crc=%04X want=C7CF", got)
	}
	// Teltonika's documented Codec 8 example packet decodes with its IO elements.
	packet, _ := hex.DecodeString("000000000000003608010000016B40D8EA30010000000000000000000000000000000105021503010101425E0F01F10000601A014E0000000000000000010000C7CF")
	codec, records, err := Decode(packet)
	if err != nil || codec != Codec8 || len(records) != 1 {
		t.Fatalf("codec=%x records=%d err=%v", codec, len(records), err)
	}
	if v, _ := records[0].Value(66); v != 0x5E0F {
		t.Fatalf("IO 66=%d", v)
	}
}

func TestFleetRecordsRoundTripInBothCodecs(t *testing.T) {
	base := time.Date(2026, 9, 30, 8, 0, 0, 123_000_000, time.UTC)
	var records []Record
	for i := 0; i < 3; i++ {
		records = append(records, FromTelemetry(telemetry.Record{
			HardwareID: "359815020000017", Time: base.Add(time.Duration(i) * 5 * time.Second), Priority: i % 2, EventIO: 239 * (i % 2),
			Position: &telemetry.Position{Lat: -2.0231234, Lon: 104.0654321, AltitudeM: 24, SpeedKmh: 7.6, HeadingDeg: 359.7, Satellites: 12, FixValid: true},
			Attributes: map[string]any{"ignition": true, "movement": true, "din1": i != 1, "gsm_signal": 4, "gnss_status": 1,
				"external_voltage": 13.87, "battery_voltage": 4.08, "odometer": 1834221.4, "power_cut": false},
		}))
	}
	for _, codec := range []byte{Codec8, Codec8E} {
		packet, err := Encode(codec, records)
		if err != nil {
			t.Fatal(err)
		}
		got, decoded, err := Decode(packet)
		if err != nil || got != codec || len(decoded) != 3 {
			t.Fatalf("codec %x: got=%x n=%d err=%v", codec, got, len(decoded), err)
		}
		r := decoded[1]
		if !r.Timestamp.Equal(base.Add(5*time.Second)) || r.Latitude != -2.0231234 || r.Longitude != 104.0654321 || r.Angle != 0 || r.Speed != 8 || r.Satellites != 12 || r.Priority != 1 || r.EventIO != 239 {
			t.Fatalf("codec %x record %+v", codec, r)
		}
		want := map[uint16]uint64{1: 0, 16: 1834221, 21: 4, 66: 13870, 67: 4080, 69: 1, 239: 1, 240: 1}
		for id, v := range want {
			if got, ok := r.Value(id); !ok || got != v {
				t.Fatalf("codec %x IO %d=%d (%v), want %d", codec, id, got, ok, v)
			}
		}
		if len(r.IO) != len(want) {
			t.Fatalf("IO elements %v: power_cut has no FMC IO element and must not be sent", r.IO)
		}
	}
}

func TestDecodeRejectsBadCRC(t *testing.T) {
	packet, err := Encode(Codec8E, []Record{{Timestamp: time.Now(), Latitude: -2.02, Longitude: 104.06}})
	if err != nil {
		t.Fatal(err)
	}
	packet[len(packet)-1] ^= 0xff
	if _, _, err := Decode(packet); err == nil {
		t.Fatal("expected CRC error")
	}
}

func TestRecordWithoutFixSendsNoSatellites(t *testing.T) {
	r := FromTelemetry(telemetry.Record{HardwareID: "359815020000017", Time: time.Now(), Position: &telemetry.Position{Lat: -2, Lon: 104, FixValid: false}})
	if r.Satellites != 0 || r.Latitude != 0 || r.Longitude != 0 {
		t.Fatalf("no-fix record %+v", r)
	}
}
