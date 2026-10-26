// Package rendezvous implements a RustDesk-compatible rendezvous (ID) server
// and relay server, wire-compatible with the stock RustDesk client.
package rendezvous

import (
	"bufio"
	"encoding/binary"
	"errors"
	"io"
)

// MaxFrame bounds a single frame on rendezvous connections. Rendezvous
// messages are small; the limit keeps a peer from making us buffer much.
const MaxFrame = 64 << 10

var errFrameTooBig = errors.New("frame too big")

// RustDesk frames every TCP message with a 1-4 byte little-endian header
// whose two low bits hold (header length - 1) and whose remaining bits hold
// the payload length.

func appendFrameHeader(dst []byte, n int) []byte {
	switch {
	case n <= 0x3F:
		return append(dst, byte(n<<2))
	case n <= 0x3FFF:
		return binary.LittleEndian.AppendUint16(dst, uint16(n<<2)|1)
	case n <= 0x3FFFFF:
		h := uint32(n<<2) | 2
		return append(dst, byte(h), byte(h>>8), byte(h>>16))
	default:
		return binary.LittleEndian.AppendUint32(dst, uint32(n<<2)|3)
	}
}

func writeFrame(w io.Writer, payload []byte) error {
	buf := appendFrameHeader(make([]byte, 0, 4+len(payload)), len(payload))
	_, err := w.Write(append(buf, payload...))
	return err
}

func readFrame(r *bufio.Reader, max int) ([]byte, error) {
	first, err := r.ReadByte()
	if err != nil {
		return nil, err
	}
	headLen := int(first&3) + 1
	n := int(first)
	for i := 1; i < headLen; i++ {
		b, err := r.ReadByte()
		if err != nil {
			return nil, io.ErrUnexpectedEOF
		}
		n |= int(b) << (8 * i)
	}
	n >>= 2
	if n > max {
		return nil, errFrameTooBig
	}
	buf := make([]byte, n)
	if _, err := io.ReadFull(r, buf); err != nil {
		return nil, io.ErrUnexpectedEOF
	}
	return buf, nil
}
