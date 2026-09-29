package teltonika

import (
	"encoding/binary"
	"fmt"
	"math"
	"time"
)

const codec8 byte = 0x08

func EncodeCodec8(record Record) ([]byte, error) {
	if record.Latitude < -90 || record.Latitude > 90 || record.Longitude < -180 || record.Longitude > 180 {
		return nil, fmt.Errorf("coordinates outside supported range")
	}
	if record.Speed < 0 || record.Speed > 65535 || record.Heading < 0 || record.Heading >= 360 {
		return nil, fmt.Errorf("speed or heading outside supported range")
	}
	if record.Timestamp.IsZero() {
		record.Timestamp = time.Now().UTC()
	}
	data := []byte{codec8, 1}
	data = appendU64(data, uint64(record.Timestamp.UnixMilli()))
	data = append(data, 0)
	data = appendI32(data, int32(math.Round(record.Longitude*1e7)))
	data = appendI32(data, int32(math.Round(record.Latitude*1e7)))
	data = appendU16(data, 0)
	data = appendU16(data, uint16(math.Round(record.Heading)))
	data = append(data, 10)
	data = appendU16(data, uint16(math.Round(record.Speed)))
	data = append(data, 239, 1, 1, 239)
	if record.Ignition {
		data = append(data, 1)
	} else {
		data = append(data, 0)
	}
	data = append(data, 0, 0, 0, 1)
	packet := make([]byte, 8, 8+len(data)+4)
	binary.BigEndian.PutUint32(packet[4:8], uint32(len(data)))
	packet = append(packet, data...)
	crc := crc16IBM(data)
	packet = append(packet, 0, 0, byte(crc>>8), byte(crc))
	return packet, nil
}
