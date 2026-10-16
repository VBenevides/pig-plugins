// Package cursordirect implements Cursor's non-public agent protocol without a CLI.
// Wire layouts and history/tool handoff adapted from Oh My Pi, MIT licensed;
// see LICENSE and docs/cursor-direct-protocol.md for the pinned source revision.
package cursordirect

import (
	"encoding/binary"
	"errors"
	"fmt"
	"io"
)

const maxFrame = 8 << 20
const maxSession = 64 << 20

type field struct {
	number int
	data   []byte
	value  uint64
	wire   int
}
type message []field

func bytesField(n int, b []byte) []byte {
	out := binary.AppendUvarint(nil, uint64(n<<3|2))
	out = binary.AppendUvarint(out, uint64(len(b)))
	return append(out, b...)
}
func str(n int, s string) []byte { return bytesField(n, []byte(s)) }
func integer(n int, v uint64) []byte {
	return binary.AppendUvarint(binary.AppendUvarint(nil, uint64(n<<3)), v)
}
func join(parts ...[]byte) []byte {
	var b []byte
	for _, p := range parts {
		b = append(b, p...)
	}
	return b
}
func parse(b []byte) (message, error) {
	if len(b) > maxFrame {
		return nil, errors.New("cursor: oversized protobuf message")
	}
	var out message
	for len(b) > 0 {
		tag, n := binary.Uvarint(b)
		if n <= 0 || tag>>3 == 0 || tag>>3 > 536870911 {
			return nil, errors.New("cursor: invalid protobuf tag")
		}
		b = b[n:]
		f := field{number: int(tag >> 3), wire: int(tag & 7)}
		switch f.wire {
		case 0:
			v, n := binary.Uvarint(b)
			if n <= 0 {
				return nil, errors.New("cursor: invalid protobuf integer")
			}
			f.value = v
			b = b[n:]
		case 2:
			size, n := binary.Uvarint(b)
			if n <= 0 || size > uint64(len(b)-n) {
				return nil, errors.New("cursor: truncated protobuf field")
			}
			b = b[n:]
			f.data = b[:int(size)]
			b = b[int(size):]
		case 1, 5:
			size := 8
			if f.wire == 5 {
				size = 4
			}
			if len(b) < size {
				return nil, io.ErrUnexpectedEOF
			}
			f.data = b[:size]
			b = b[size:]
		default:
			return nil, errors.New("cursor: unsupported protobuf wire type")
		}
		out = append(out, f)
		if len(out) > 100000 {
			return nil, errors.New("cursor: too many protobuf fields")
		}
	}
	return out, nil
}
func (m message) data(n int) []byte {
	for _, f := range m {
		if f.number == n && f.wire == 2 {
			return f.data
		}
	}
	return nil
}
func (m message) text(n int) string { return string(m.data(n)) }
func (m message) num(n int) uint64 {
	for _, f := range m {
		if f.number == n && f.wire == 0 {
			return f.value
		}
	}
	return 0
}
func (m message) has(n int) bool {
	for _, f := range m {
		if f.number == n {
			return true
		}
	}
	return false
}
func frame(b []byte) []byte {
	out := make([]byte, 5, len(b)+5)
	binary.BigEndian.PutUint32(out[1:], uint32(len(b)))
	return append(out, b...)
}
func readFrame(r io.Reader) (byte, []byte, error) {
	var head [5]byte
	if _, err := io.ReadFull(r, head[:]); err != nil {
		return 0, nil, err
	}
	size := binary.BigEndian.Uint32(head[1:])
	if size > maxFrame {
		return 0, nil, fmt.Errorf("cursor: frame exceeds %d bytes", maxFrame)
	}
	if head[0] != 0 && head[0] != 2 {
		return 0, nil, errors.New("cursor: unsupported compressed/flagged frame")
	}
	b := make([]byte, int(size))
	_, err := io.ReadFull(r, b)
	return head[0], b, err
}
