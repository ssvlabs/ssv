package wire

import (
	"encoding/binary"
	"fmt"
)

// This file provides the low-level, domain-agnostic read/write primitives used
// to (de)serialize length-prefixed, big-endian binary messages. Domain wire
// packages (e.g. obft/base/wire, obft/twoab/wire) build their per-message
// Encode/Decode on top of these; the size/count caps and message layouts stay
// in the domain packages. Centralizing the primitives keeps the framing,
// truncation handling, and defensive-copy discipline identical across domains
// (and prevents the per-field-cap bound from drifting between call sites).

// AppendUint16 appends v to b as 2 big-endian bytes.
func AppendUint16(b []byte, v uint16) []byte {
	var buf [2]byte
	binary.BigEndian.PutUint16(buf[:], v)
	return append(b, buf[:]...)
}

// AppendUint32 appends v to b as 4 big-endian bytes.
func AppendUint32(b []byte, v uint32) []byte {
	var buf [4]byte
	binary.BigEndian.PutUint32(buf[:], v)
	return append(b, buf[:]...)
}

// AppendUint64 appends v to b as 8 big-endian bytes.
func AppendUint64(b []byte, v uint64) []byte {
	var buf [8]byte
	binary.BigEndian.PutUint64(buf[:], v)
	return append(b, buf[:]...)
}

// Reader is a forward-only cursor over a byte buffer for decoding
// length-prefixed, big-endian wire messages. Every method takes a `name`
// used in truncation error messages. Not safe for concurrent use.
type Reader struct {
	data []byte
	pos  int
}

// NewReader returns a Reader positioned at the start of data.
func NewReader(data []byte) *Reader { return &Reader{data: data} }

// Remaining reports the number of unconsumed bytes.
func (r *Reader) Remaining() int { return len(r.data) - r.pos }

// Byte reads a single byte.
func (r *Reader) Byte(name string) (byte, error) {
	if r.Remaining() < 1 {
		return 0, fmt.Errorf("wire: truncated reading %s", name)
	}
	b := r.data[r.pos]
	r.pos++
	return b, nil
}

// Uint16 reads a big-endian uint16.
func (r *Reader) Uint16(name string) (uint16, error) {
	if r.Remaining() < 2 {
		return 0, fmt.Errorf("wire: truncated reading %s", name)
	}
	v := binary.BigEndian.Uint16(r.data[r.pos : r.pos+2])
	r.pos += 2
	return v, nil
}

// Uint32 reads a big-endian uint32.
func (r *Reader) Uint32(name string) (uint32, error) {
	if r.Remaining() < 4 {
		return 0, fmt.Errorf("wire: truncated reading %s", name)
	}
	v := binary.BigEndian.Uint32(r.data[r.pos : r.pos+4])
	r.pos += 4
	return v, nil
}

// Uint64 reads a big-endian uint64.
func (r *Reader) Uint64(name string) (uint64, error) {
	if r.Remaining() < 8 {
		return 0, fmt.Errorf("wire: truncated reading %s", name)
	}
	v := binary.BigEndian.Uint64(r.data[r.pos : r.pos+8])
	r.pos += 8
	return v, nil
}

// Bytes reads exactly n bytes and returns a defensive copy that never aliases
// the underlying buffer.
func (r *Reader) Bytes(n int, name string) ([]byte, error) {
	if n < 0 {
		return nil, fmt.Errorf("wire: negative length reading %s", name)
	}
	if r.Remaining() < n {
		return nil, fmt.Errorf("wire: truncated reading %s (need %d, have %d)", name, n, r.Remaining())
	}
	out := make([]byte, n)
	copy(out, r.data[r.pos:r.pos+n])
	r.pos += n
	return out, nil
}

// FixedBytes reads exactly len(out) bytes into out (for fixed-size fields such
// as a [32]byte cluster id or a tag).
func (r *Reader) FixedBytes(out []byte, name string) error {
	n := len(out)
	if r.Remaining() < n {
		return fmt.Errorf("wire: truncated reading %s (need %d, have %d)", name, n, r.Remaining())
	}
	copy(out, r.data[r.pos:r.pos+n])
	r.pos += n
	return nil
}

// LengthPrefixed reads a uint32 length, rejects it if it exceeds maxLen, then
// reads that many bytes (defensive copy). This is the single choke point for
// length-prefixed field reads: callers pass the per-field cap, so the bound
// cannot drift between call sites.
func (r *Reader) LengthPrefixed(name string, maxLen int) ([]byte, error) {
	n, err := r.Uint32(name + " length")
	if err != nil {
		return nil, err
	}
	if int64(n) > int64(maxLen) {
		return nil, fmt.Errorf("wire: %s too long (%d, max %d)", name, n, maxLen)
	}
	return r.Bytes(int(n), name)
}

// RequireEOF returns an error if any bytes remain unconsumed.
func (r *Reader) RequireEOF(name string) error {
	if r.Remaining() != 0 {
		return fmt.Errorf("wire: %s has %d trailing bytes", name, r.Remaining())
	}
	return nil
}
