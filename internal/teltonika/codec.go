// Package teltonika speaks Teltonika's TCP protocol as an FMB/FMC tracker does: the IMEI
// handshake, then AVL data packets in Codec 8 (0x08) or Codec 8 Extended (0x8E), each
// acknowledged with its record count.
package teltonika

import (
	"encoding/binary"
	"fmt"
	"math"
	"sort"
	"time"
)

// Codec IDs.
const (
	Codec8  byte = 0x08
	Codec8E byte = 0x8e
)

// Record is one AVL record.
type Record struct {
	Timestamp  time.Time
	Priority   byte // 0 low, 1 high, 2 panic
	Longitude  float64
	Latitude   float64
	Altitude   int // metres
	Angle      int // degrees, 0 to 359
	Satellites int
	Speed      int // km/h
	EventIO    uint16
	IO         []IO
}

// IO is one IO element: its ID, its width in bytes (1, 2, 4 or 8) and its value.
type IO struct {
	ID    uint16
	Size  int
	Value uint64
}

// Value returns an IO element's value by ID.
func (r Record) Value(id uint16) (uint64, bool) {
	for _, io := range r.IO {
		if io.ID == id {
			return io.Value, true
		}
	}
	return 0, false
}

var sizes = []int{1, 2, 4, 8}

// Encode builds one AVL data packet holding records, at most 255.
func Encode(codec byte, records []Record) ([]byte, error) {
	if codec != Codec8 && codec != Codec8E {
		return nil, fmt.Errorf("unsupported codec 0x%02x", codec)
	}
	if len(records) == 0 || len(records) > 255 {
		return nil, fmt.Errorf("a packet holds 1 to 255 records, not %d", len(records))
	}
	data := []byte{codec, byte(len(records))}
	for _, r := range records {
		if r.Latitude < -90 || r.Latitude > 90 || r.Longitude < -180 || r.Longitude > 180 {
			return nil, fmt.Errorf("coordinates outside supported range")
		}
		if r.Speed < 0 || r.Speed > 65535 || r.Angle < 0 || r.Angle >= 360 {
			return nil, fmt.Errorf("speed or heading outside supported range")
		}
		ts := r.Timestamp
		if ts.IsZero() {
			ts = time.Now()
		}
		data = binary.BigEndian.AppendUint64(data, uint64(ts.UnixMilli()))
		data = append(data, r.Priority)
		data = binary.BigEndian.AppendUint32(data, uint32(int32(math.Round(r.Longitude*1e7))))
		data = binary.BigEndian.AppendUint32(data, uint32(int32(math.Round(r.Latitude*1e7))))
		data = binary.BigEndian.AppendUint16(data, uint16(int16(r.Altitude)))
		data = binary.BigEndian.AppendUint16(data, uint16(r.Angle))
		data = append(data, byte(r.Satellites))
		data = binary.BigEndian.AppendUint16(data, uint16(r.Speed))
		groups := map[int][]IO{}
		for _, io := range r.IO {
			if codec == Codec8 && io.ID > 255 {
				return nil, fmt.Errorf("IO %d does not fit Codec 8", io.ID)
			}
			groups[io.Size] = append(groups[io.Size], io)
		}
		for _, s := range sizes {
			sort.Slice(groups[s], func(i, j int) bool { return groups[s][i].ID < groups[s][j].ID })
		}
		if len(groups) > 0 {
			for s := range groups {
				if s != 1 && s != 2 && s != 4 && s != 8 {
					return nil, fmt.Errorf("IO element width %d", s)
				}
			}
		}
		if codec == Codec8E {
			data = binary.BigEndian.AppendUint16(data, r.EventIO)
			data = binary.BigEndian.AppendUint16(data, uint16(len(r.IO)))
			for _, s := range sizes {
				data = binary.BigEndian.AppendUint16(data, uint16(len(groups[s])))
				for _, io := range groups[s] {
					data = binary.BigEndian.AppendUint16(data, io.ID)
					data = appendValue(data, s, io.Value)
				}
			}
			data = binary.BigEndian.AppendUint16(data, 0) // no variable-size elements
		} else {
			data = append(data, byte(r.EventIO), byte(len(r.IO)))
			for _, s := range sizes {
				data = append(data, byte(len(groups[s])))
				for _, io := range groups[s] {
					data = append(data, byte(io.ID))
					data = appendValue(data, s, io.Value)
				}
			}
		}
	}
	data = append(data, byte(len(records)))
	packet := make([]byte, 8, 8+len(data)+4)
	binary.BigEndian.PutUint32(packet[4:8], uint32(len(data)))
	packet = append(packet, data...)
	return binary.BigEndian.AppendUint32(packet, uint32(crc16IBM(data))), nil
}

func appendValue(dst []byte, size int, v uint64) []byte {
	switch size {
	case 1:
		return append(dst, byte(v))
	case 2:
		return binary.BigEndian.AppendUint16(dst, uint16(v))
	case 4:
		return binary.BigEndian.AppendUint32(dst, uint32(v))
	}
	return binary.BigEndian.AppendUint64(dst, v)
}

// Decode reads an AVL data packet back into its codec and records. The simulator uses it to
// test its encoder; Hexa.Sensor has its own decoder.
func Decode(packet []byte) (byte, []Record, error) {
	if len(packet) < 12 || binary.BigEndian.Uint32(packet[:4]) != 0 {
		return 0, nil, fmt.Errorf("invalid AVL preamble")
	}
	n := int(binary.BigEndian.Uint32(packet[4:8]))
	if n < 3 || len(packet) != 8+n+4 {
		return 0, nil, fmt.Errorf("invalid AVL data length")
	}
	data := packet[8 : 8+n]
	if crc := binary.BigEndian.Uint32(packet[8+n:]); crc > math.MaxUint16 || uint16(crc) != crc16IBM(data) {
		return 0, nil, fmt.Errorf("invalid AVL CRC")
	}
	codec, count := data[0], int(data[1])
	if codec != Codec8 && codec != Codec8E {
		return 0, nil, fmt.Errorf("unsupported codec 0x%02x", codec)
	}
	if int(data[len(data)-1]) != count {
		return 0, nil, fmt.Errorf("record counts differ")
	}
	d := &reader{b: data[:len(data)-1], pos: 2}
	var out []Record
	for i := 0; i < count; i++ {
		r := Record{
			Timestamp: time.UnixMilli(int64(d.u(8))).UTC(), Priority: byte(d.u(1)),
			Longitude: float64(int32(d.u(4))) / 1e7, Latitude: float64(int32(d.u(4))) / 1e7,
			Altitude: int(int16(d.u(2))), Angle: int(d.u(2)), Satellites: int(d.u(1)), Speed: int(d.u(2)),
		}
		width := 1
		if codec == Codec8E {
			width = 2
		}
		r.EventIO = uint16(d.u(width))
		d.u(width) // total
		for _, s := range sizes {
			for k := int(d.u(width)); k > 0; k-- {
				id := uint16(d.u(width))
				r.IO = append(r.IO, IO{ID: id, Size: s, Value: d.u(s)})
			}
		}
		if codec == Codec8E {
			for k := int(d.u(2)); k > 0; k-- {
				d.u(2)
				d.pos += int(d.u(2))
			}
		}
		if d.err != nil {
			return 0, nil, d.err
		}
		out = append(out, r)
	}
	if d.pos != len(d.b) {
		return 0, nil, fmt.Errorf("trailing bytes in AVL data")
	}
	return codec, out, nil
}

type reader struct {
	b   []byte
	pos int
	err error
}

func (r *reader) u(n int) uint64 {
	if r.err != nil || r.pos+n > len(r.b) {
		r.err = fmt.Errorf("truncated AVL record")
		return 0
	}
	var v uint64
	for _, c := range r.b[r.pos : r.pos+n] {
		v = v<<8 | uint64(c)
	}
	r.pos += n
	return v
}

// EncodeIMEI frames the handshake: the IMEI's length and its digits.
func EncodeIMEI(imei string) ([]byte, error) {
	if len(imei) != 15 {
		return nil, fmt.Errorf("Teltonika IMEI must contain exactly 15 digits")
	}
	for _, r := range imei {
		if r < '0' || r > '9' {
			return nil, fmt.Errorf("Teltonika IMEI must contain only digits")
		}
	}
	out := binary.BigEndian.AppendUint16(nil, uint16(len(imei)))
	return append(out, imei...), nil
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
