package teltonika

import (
	"encoding/binary"
	"fmt"
	"math"
	"time"
)

// DecodeCodec8Extended decodes the single-record Codec 8 Extended subset emitted by this simulator.
func DecodeCodec8Extended(packet []byte) (Record, error) {
	var zero Record
	if len(packet) < 12 {
		return zero, fmt.Errorf("AVL packet too short")
	}
	if binary.BigEndian.Uint32(packet[:4]) != 0 {
		return zero, fmt.Errorf("invalid AVL preamble")
	}
	dataLen := int(binary.BigEndian.Uint32(packet[4:8]))
	if dataLen <= 0 || len(packet) != 8+dataLen+4 {
		return zero, fmt.Errorf("invalid AVL data length")
	}
	data := packet[8 : 8+dataLen]
	wireCRC := binary.BigEndian.Uint32(packet[8+dataLen:])
	if wireCRC > math.MaxUint16 || uint16(wireCRC) != crc16IBM(data) {
		return zero, fmt.Errorf("invalid AVL CRC")
	}
	if len(data) < 2 || data[0] != codec8Extended || data[1] != 1 || data[len(data)-1] != 1 {
		return zero, fmt.Errorf("unsupported Codec 8 Extended record count")
	}

	pos := 2
	need := func(n int) error {
		if pos+n > len(data)-1 {
			return fmt.Errorf("truncated Codec 8 Extended record")
		}
		return nil
	}
	if err := need(8 + 1 + 4 + 4 + 2 + 2 + 1 + 2); err != nil {
		return zero, err
	}
	timestamp := int64(binary.BigEndian.Uint64(data[pos : pos+8]))
	pos += 8
	pos++ // priority
	longitude := int32(binary.BigEndian.Uint32(data[pos : pos+4]))
	pos += 4
	latitude := int32(binary.BigEndian.Uint32(data[pos : pos+4]))
	pos += 4
	pos += 2 // altitude
	heading := binary.BigEndian.Uint16(data[pos : pos+2])
	pos += 2
	pos++ // satellites
	speed := binary.BigEndian.Uint16(data[pos : pos+2])
	pos += 2

	if err := need(4); err != nil {
		return zero, err
	}
	pos += 2 // event IO ID
	totalIO := int(binary.BigEndian.Uint16(data[pos : pos+2]))
	pos += 2
	ignition := false
	seenIO := 0
	for _, width := range []int{1, 2, 4, 8} {
		if err := need(2); err != nil {
			return zero, err
		}
		count := int(binary.BigEndian.Uint16(data[pos : pos+2]))
		pos += 2
		for i := 0; i < count; i++ {
			if err := need(2 + width); err != nil {
				return zero, err
			}
			id := binary.BigEndian.Uint16(data[pos : pos+2])
			pos += 2
			value := data[pos : pos+width]
			pos += width
			if id == 239 && width == 1 {
				ignition = value[0] != 0
			}
			seenIO++
		}
	}
	if err := need(2); err != nil {
		return zero, err
	}
	variableCount := int(binary.BigEndian.Uint16(data[pos : pos+2]))
	pos += 2
	for i := 0; i < variableCount; i++ {
		if err := need(4); err != nil {
			return zero, err
		}
		pos += 2 // id
		length := int(binary.BigEndian.Uint16(data[pos : pos+2]))
		pos += 2
		if err := need(length); err != nil {
			return zero, err
		}
		pos += length
		seenIO++
	}
	if seenIO != totalIO || pos != len(data)-1 {
		return zero, fmt.Errorf("invalid Codec 8 Extended IO section")
	}
	return Record{
		Timestamp: time.UnixMilli(timestamp).UTC(),
		Latitude:  float64(latitude) / 1e7,
		Longitude: float64(longitude) / 1e7,
		Speed:     float64(speed),
		Heading:   float64(heading),
		Ignition:  ignition,
	}, nil
}
