package wire

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// ---- Append* writers ----

func TestAppendUintN_BigEndianAndAppends(t *testing.T) {
	require.Equal(t, []byte{0x01, 0x02}, AppendUint16(nil, 0x0102))
	require.Equal(t, []byte{0x01, 0x02, 0x03, 0x04}, AppendUint32(nil, 0x01020304))
	require.Equal(t,
		[]byte{0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07, 0x08},
		AppendUint64(nil, 0x0102030405060708))

	// Append must extend, not overwrite, an existing prefix.
	require.Equal(t, []byte{0xFF, 0x01, 0x02}, AppendUint16([]byte{0xFF}, 0x0102))
}

func TestAppendRead_RoundTrip(t *testing.T) {
	buf := AppendUint16(nil, 0x0102)
	buf = AppendUint32(buf, 0x03040506)
	buf = AppendUint64(buf, 0x0708090A0B0C0D0E)

	r := NewReader(buf)
	u16, err := r.Uint16("u16")
	require.NoError(t, err)
	require.Equal(t, uint16(0x0102), u16)
	u32, err := r.Uint32("u32")
	require.NoError(t, err)
	require.Equal(t, uint32(0x03040506), u32)
	u64, err := r.Uint64("u64")
	require.NoError(t, err)
	require.Equal(t, uint64(0x0708090A0B0C0D0E), u64)
	require.NoError(t, r.RequireEOF("end"))
}

// ---- cursor bookkeeping ----

func TestReader_RemainingTracksConsumption(t *testing.T) {
	r := NewReader([]byte{1, 2, 3, 4, 5})
	require.Equal(t, 5, r.Remaining())
	_, err := r.Uint16("u16")
	require.NoError(t, err)
	require.Equal(t, 3, r.Remaining())
	_, err = r.Byte("b")
	require.NoError(t, err)
	require.Equal(t, 2, r.Remaining())
}

// ---- fixed-width reads: values and truncation ----

func TestReader_FixedWidth_Truncation(t *testing.T) {
	_, err := NewReader(nil).Byte("b")
	require.ErrorContains(t, err, "truncated reading b")
	_, err = NewReader([]byte{0x01}).Uint16("u16")
	require.ErrorContains(t, err, "truncated reading u16")
	_, err = NewReader([]byte{0x01, 0x02, 0x03}).Uint32("u32")
	require.ErrorContains(t, err, "truncated reading u32")
	_, err = NewReader([]byte{1, 2, 3, 4, 5, 6, 7}).Uint64("u64")
	require.ErrorContains(t, err, "truncated reading u64")
}

// ---- Bytes ----

func TestReader_Bytes_ZeroLengthAndNegative(t *testing.T) {
	got, err := NewReader(nil).Bytes(0, "f")
	require.NoError(t, err)
	require.Empty(t, got)

	_, err = NewReader([]byte{1, 2, 3}).Bytes(-1, "f")
	require.ErrorContains(t, err, "negative length reading f")
}

func TestReader_Bytes_Truncation(t *testing.T) {
	_, err := NewReader([]byte{1, 2}).Bytes(5, "f")
	require.ErrorContains(t, err, "truncated reading f (need 5, have 2)")
}

// TestReader_Bytes_DefensiveCopy is the core invariant: a slice returned by
// Bytes must never alias the source buffer, so a caller mutating the source
// (or the wire decoder reusing it) cannot corrupt already-decoded fields.
func TestReader_Bytes_DefensiveCopy(t *testing.T) {
	src := []byte{0x01, 0x02, 0x03, 0x04}
	got, err := NewReader(src).Bytes(4, "f")
	require.NoError(t, err)
	require.Equal(t, []byte{0x01, 0x02, 0x03, 0x04}, got)

	src[0] = 0xFF
	require.Equal(t, byte(0x01), got[0], "Bytes must return a copy that does not alias the source")
}

// ---- FixedBytes ----

func TestReader_FixedBytes_ReadsAndTruncates(t *testing.T) {
	var out [4]byte
	require.NoError(t, NewReader([]byte{1, 2, 3, 4, 5}).FixedBytes(out[:], "f"))
	require.Equal(t, [4]byte{1, 2, 3, 4}, out)

	err := NewReader([]byte{1, 2}).FixedBytes(out[:], "f")
	require.ErrorContains(t, err, "truncated reading f (need 4, have 2)")
}

func TestReader_FixedBytes_DefensiveCopy(t *testing.T) {
	src := []byte{0x01, 0x02, 0x03, 0x04}
	var out [4]byte
	require.NoError(t, NewReader(src).FixedBytes(out[:], "f"))
	src[0] = 0xFF
	require.Equal(t, byte(0x01), out[0], "FixedBytes must copy out of the source buffer")
}

// ---- LengthPrefixed ----

func TestReader_LengthPrefixed_MaxLenBoundary(t *testing.T) {
	// n == maxLen is accepted (inclusive bound).
	buf := append(AppendUint32(nil, 3), 'a', 'b', 'c')
	got, err := NewReader(buf).LengthPrefixed("f", 3)
	require.NoError(t, err)
	require.Equal(t, []byte("abc"), got)

	// n == maxLen+1 is rejected.
	buf = append(AppendUint32(nil, 4), 'a', 'b', 'c', 'd')
	_, err = NewReader(buf).LengthPrefixed("f", 3)
	require.ErrorContains(t, err, "f too long (4, max 3)")
}

// TestReader_LengthPrefixed_RejectsOversizeBeforeAlloc proves the cap is
// checked against the declared length *before* the body is read — so a
// malformed message declaring a huge field cannot drive an allocation. The
// buffer here carries no body bytes at all, yet the call fails with "too long"
// (a cap rejection) rather than "truncated" (a post-cap body read).
func TestReader_LengthPrefixed_RejectsOversizeBeforeAlloc(t *testing.T) {
	buf := AppendUint32(nil, 1<<30) // 1 GiB declared, zero body bytes present
	_, err := NewReader(buf).LengthPrefixed("f", 64*1024)
	require.ErrorContains(t, err, "too long")
}

func TestReader_LengthPrefixed_Truncation(t *testing.T) {
	// Length prefix itself truncated (<4 bytes).
	_, err := NewReader([]byte{0x00, 0x01}).LengthPrefixed("f", 100)
	require.ErrorContains(t, err, "truncated reading f length")

	// Length declares 10 bytes, body has only 2.
	buf := append(AppendUint32(nil, 10), 0x01, 0x02)
	_, err = NewReader(buf).LengthPrefixed("f", 100)
	require.ErrorContains(t, err, "truncated reading f (need 10, have 2)")
}

func TestReader_LengthPrefixed_DefensiveCopy(t *testing.T) {
	src := append(AppendUint32(nil, 3), 'a', 'b', 'c')
	got, err := NewReader(src).LengthPrefixed("f", 16)
	require.NoError(t, err)
	require.Equal(t, []byte("abc"), got)

	src[4] = 'X' // mutate the body region of the source
	require.Equal(t, []byte("abc"), got, "LengthPrefixed must return a non-aliasing copy")
}

// ---- RequireEOF ----

func TestReader_RequireEOF(t *testing.T) {
	require.NoError(t, NewReader(nil).RequireEOF("x"))

	err := NewReader([]byte{0x01}).RequireEOF("x")
	require.ErrorContains(t, err, "x has 1 trailing bytes")

	// EOF holds once everything is consumed.
	r := NewReader([]byte{0x01})
	_, err = r.Byte("b")
	require.NoError(t, err)
	require.NoError(t, r.RequireEOF("x"))
}

// ---- Expect* header helpers ----

func TestReader_ExpectByte(t *testing.T) {
	require.NoError(t, NewReader([]byte{0x01}).ExpectByte(0x01, "inner kind"))

	err := NewReader([]byte{0x02}).ExpectByte(0x01, "inner kind")
	require.ErrorContains(t, err, "inner kind mismatch")

	err = NewReader(nil).ExpectByte(0x01, "inner kind")
	require.ErrorContains(t, err, "truncated reading inner kind")
}

func TestReader_ExpectBytes(t *testing.T) {
	tag := []byte("TAG")
	require.NoError(t, NewReader([]byte("TAG")).ExpectBytes(tag, "protocol tag"))

	err := NewReader([]byte("TAX")).ExpectBytes(tag, "protocol tag")
	require.ErrorContains(t, err, "protocol tag mismatch")

	err = NewReader([]byte("TA")).ExpectBytes(tag, "protocol tag")
	require.ErrorContains(t, err, "truncated reading protocol tag")
}

func TestReader_ExpectVersion(t *testing.T) {
	require.NoError(t, NewReader([]byte{0x03}).ExpectVersion(0x03, "msg"))

	err := NewReader([]byte{0x02}).ExpectVersion(0x03, "msg")
	require.ErrorContains(t, err, "unsupported msg version 0x02")

	err = NewReader(nil).ExpectVersion(0x03, "msg")
	require.ErrorContains(t, err, "truncated reading msg version")
}
