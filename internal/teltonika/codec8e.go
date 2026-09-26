package teltonika

import (
	"encoding/binary"
	"fmt"
	"math"
	"time"
)

const codec8Extended byte = 0x8e

// Record is the minimal AVL state emitted by the simulator.
type Record struct {
	Timestamp time.Time
	Latitude  float64
	Longitude float64
	Speed     float64
	Heading   float64
	Ignition  bool
}

func EncodeCodec8Extended(record Record) ([]byte, error) {
	if record.Latitude < -90 || record.Latitude > 90 || record.Longitude < -180 || record.Longitude > 180 {
		return nil, fmt.Errorf("coordinates outside supported range")
	}
	if record.Speed < 0 || record.Speed > 65535 || record.Heading < 0 || record.Heading >= 360 {
		return nil, fmt.Errorf("speed or heading outside supported range")
	}
	if record.Timestamp.IsZero() {
		record.Timestamp = time.Now().UTC()
	}

	data := make([]byte, 0, 64)
	data = append(data, codec8Extended, 0x01)
	data = appendU64(data, uint64(record.Timestamp.UnixMilli()))
	data = append(data, 0x00) // normal priority
	data = appendI32(data, int32(math.Round(record.Longitude*1e7)))
	data = appendI32(data, int32(math.Round(record.Latitude*1e7)))
	data = appendU16(data, 0) // altitude unknown in the current simulator model
	data = appendU16(data, uint16(math.Round(record.Heading)))
	data = append(data, 10) // deterministic demo satellite count
	data = appendU16(data, uint16(math.Round(record.Speed)))

	// Codec 8 Extended IO element. AVL ID 239 is the ignition flag.
	data = appendU16(data, 239) // event IO ID
	data = appendU16(data, 1)   // total IO elements
	data = appendU16(data, 1)   // one-byte IO count
	data = appendU16(data, 239)
	if record.Ignition {
		data = append(data, 1)
	} else {
		data = append(data, 0)
	}
	data = appendU16(data, 0) // two-byte IO count
	data = appendU16(data, 0) // four-byte IO count
	data = appendU16(data, 0) // eight-byte IO count
	data = appendU16(data, 0) // variable-size IO count
	data = append(data, 0x01) // number of records, repeated

	packet := make([]byte, 8, 8+len(data)+4)
	binary.BigEndian.PutUint32(packet[4:8], uint32(len(data)))
	packet = append(packet, data...)
	crc := crc16IBM(data)
	packet = append(packet, 0, 0, byte(crc>>8), byte(crc))
	return packet, nil
}

func EncodeIMEI(imei string) ([]byte, error) {
	if len(imei) != 15 {
		return nil, fmt.Errorf("Teltonika IMEI must contain exactly 15 digits")
	}
	for _, r := range imei {
		if r < '0' || r > '9' {
			return nil, fmt.Errorf("Teltonika IMEI must contain only digits")
		}
	}
	out := make([]byte, 2, 2+len(imei))
	binary.BigEndian.PutUint16(out, uint16(len(imei)))
	return append(out, []byte(imei)...), nil
}

func crc16IBM(data []byte) uint16 {
	var crc uint16
	for _, value := range data {
		crc ^= uint16(value)
		for bit := 0; bit < 8; bit++ {
			if crc&1 != 0 {
				crc = (crc >> 1) ^ 0xA001
			} else {
				crc >>= 1
			}
		}
	}
	return crc
}

func appendU16(dst []byte, value uint16) []byte {
	var b [2]byte
	binary.BigEndian.PutUint16(b[:], value)
	return append(dst, b[:]...)
}
func appendU64(dst []byte, value uint64) []byte {
	var b [8]byte
	binary.BigEndian.PutUint64(b[:], value)
	return append(dst, b[:]...)
}
func appendI32(dst []byte, value int32) []byte {
	var b [4]byte
	binary.BigEndian.PutUint32(b[:], uint32(value))
	return append(dst, b[:]...)
}
