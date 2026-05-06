// Package wire provides binary serialization for OBFT messages that flow
// over the network. The encoding is intentionally simple (length-prefixed
// fields, big-endian integers, version byte) — not SSZ.
//
// The wire format is independent of any specific p2p envelope: the SSV
// adapter wraps the bytes produced here in a SignedSSVMessage. This package
// only handles the (un)marshaling of OBFT message bodies.
package wire

import (
	"encoding/binary"
	"errors"
	"fmt"

	"github.com/ssvlabs/ssv/protocol/v2/obft"
)

// Wire format versions. Bumped when the on-the-wire layout changes
// incompatibly. Encoders write the current version; decoders accept only
// versions they understand.
const (
	Phase1BundleVersionV1 byte = 0x01
	OnionVersionV1        byte = 0x01
	NRVersionV1           byte = 0x01
	CertificateVersionV1  byte = 0x01
)

// MaxLayers caps the number of layers an Onion or NR can declare on the wire.
// Real OBFT configs use K ≤ n ≤ 13 in SSV; anything past 32 is almost
// certainly malformed/malicious.
const MaxLayers = 32

// MaxFieldSize caps individual length-prefixed fields (values, ciphertexts,
// signatures). 16 MiB is far above realistic SSV proposer-duty values
// (~1 KB blinded blocks, ~96 B signatures) but still safe against unbounded
// allocation from a malformed message.
const MaxFieldSize = 16 * 1024 * 1024

// EncodePhase1Bundle serializes a Phase-1 bundle.
//
// Format (version 0x01):
//
//	[1] version
//	[8] OperatorID    (uint64 big-endian)
//	[8] Height        (uint64 big-endian)
//	[4] Layer         (uint32 big-endian)
//	[4] Value length  (uint32 big-endian)
//	[Value bytes]
//	[4] SigmaV length (uint32 big-endian)
//	[SigmaV bytes]
func EncodePhase1Bundle(b *obft.Phase1Bundle) ([]byte, error) {
	if b == nil {
		return nil, errors.New("wire: nil phase-1 bundle")
	}
	if b.Layer < 0 {
		return nil, fmt.Errorf("wire: phase-1 bundle has negative layer %d", b.Layer)
	}
	if len(b.Value) > MaxFieldSize {
		return nil, fmt.Errorf("wire: phase-1 bundle value too long (%d)", len(b.Value))
	}
	if len(b.SigmaV) > MaxFieldSize {
		return nil, fmt.Errorf("wire: phase-1 bundle SigmaV too long (%d)", len(b.SigmaV))
	}

	size := 1 + 8 + 8 + 4 + 4 + len(b.Value) + 4 + len(b.SigmaV)
	out := make([]byte, 0, size)
	out = append(out, Phase1BundleVersionV1)
	out = appendUint64(out, uint64(b.OperatorID))
	out = appendUint64(out, uint64(b.Height))
	out = appendUint32(out, uint32(b.Layer))      //nolint:gosec // bounds-checked above
	out = appendUint32(out, uint32(len(b.Value))) //nolint:gosec // bounds-checked
	out = append(out, b.Value...)
	out = appendUint32(out, uint32(len(b.SigmaV))) //nolint:gosec // bounds-checked
	out = append(out, b.SigmaV...)
	return out, nil
}

// DecodePhase1Bundle parses bytes produced by EncodePhase1Bundle.
func DecodePhase1Bundle(data []byte) (*obft.Phase1Bundle, error) {
	r := newReader(data)
	version, err := r.byte_("version")
	if err != nil {
		return nil, err
	}
	if version != Phase1BundleVersionV1 {
		return nil, fmt.Errorf("wire: unsupported phase-1 bundle version 0x%02x", version)
	}
	opID, err := r.uint64_("operator id")
	if err != nil {
		return nil, err
	}
	height, err := r.uint64_("height")
	if err != nil {
		return nil, err
	}
	layer, err := r.uint32_("layer")
	if err != nil {
		return nil, err
	}
	valueLen, err := r.uint32_("value length")
	if err != nil {
		return nil, err
	}
	if valueLen > MaxFieldSize {
		return nil, fmt.Errorf("wire: phase-1 value too long (%d)", valueLen)
	}
	value, err := r.bytes(int(valueLen), "value")
	if err != nil {
		return nil, err
	}
	sigLen, err := r.uint32_("sigmaV length")
	if err != nil {
		return nil, err
	}
	if sigLen > MaxFieldSize {
		return nil, fmt.Errorf("wire: phase-1 sigmaV too long (%d)", sigLen)
	}
	sig, err := r.bytes(int(sigLen), "sigmaV")
	if err != nil {
		return nil, err
	}
	if r.remaining() != 0 {
		return nil, fmt.Errorf("wire: %d trailing bytes after phase-1 bundle", r.remaining())
	}
	return &obft.Phase1Bundle{
		OperatorID: obft.OperatorID(opID),
		Height:     obft.Height(height),
		Layer:      int(layer),
		Value:      obft.Value(value),
		SigmaV:     obft.Signature(sig),
	}, nil
}

// EncodeOnion serializes an Onion (KindOnion payload).
//
// Format (version 0x01):
//
//	[1] version
//	[8] OperatorID         (uint64 big-endian)
//	[8] Height             (uint64 big-endian)
//	[2] number of layers   (uint16 big-endian)
//	for each layer:
//	  [4] value length      (uint32 big-endian)
//	  [value bytes]
//	  [4] ciphertext length (uint32 big-endian)
//	  [ciphertext bytes]
func EncodeOnion(o *obft.Onion) ([]byte, error) {
	if o == nil {
		return nil, errors.New("wire: nil onion")
	}
	if len(o.Layers) > MaxLayers {
		return nil, fmt.Errorf("wire: onion has %d layers (max %d)", len(o.Layers), MaxLayers)
	}

	size := 1 + 8 + 8 + 2
	for _, el := range o.Layers {
		size += 4 + len(el.Value) + 4 + len(el.Ciphertext)
	}
	out := make([]byte, 0, size)
	out = append(out, OnionVersionV1)
	out = appendUint64(out, uint64(o.OperatorID))
	out = appendUint64(out, uint64(o.Height))
	out = appendUint16(out, uint16(len(o.Layers))) //nolint:gosec // MaxLayers <= uint16 max
	for i, el := range o.Layers {
		if len(el.Value) > MaxFieldSize {
			return nil, fmt.Errorf("wire: onion layer %d value too long (%d)", i, len(el.Value))
		}
		if len(el.Ciphertext) > MaxFieldSize {
			return nil, fmt.Errorf("wire: onion layer %d ciphertext too long (%d)", i, len(el.Ciphertext))
		}
		out = appendUint32(out, uint32(len(el.Value)))      //nolint:gosec // bounds-checked
		out = append(out, el.Value...)                      //
		out = appendUint32(out, uint32(len(el.Ciphertext))) //nolint:gosec // bounds-checked
		out = append(out, el.Ciphertext...)
	}
	return out, nil
}

// DecodeOnion parses bytes produced by EncodeOnion.
func DecodeOnion(data []byte) (*obft.Onion, error) {
	r := newReader(data)
	version, err := r.byte_("version")
	if err != nil {
		return nil, err
	}
	if version != OnionVersionV1 {
		return nil, fmt.Errorf("wire: unsupported onion version 0x%02x", version)
	}
	opID, err := r.uint64_("operator id")
	if err != nil {
		return nil, err
	}
	height, err := r.uint64_("height")
	if err != nil {
		return nil, err
	}
	numLayers, err := r.uint16_("layer count")
	if err != nil {
		return nil, err
	}
	if int(numLayers) > MaxLayers {
		return nil, fmt.Errorf("wire: onion declares %d layers (max %d)", numLayers, MaxLayers)
	}
	layers := make([]obft.EncryptedLayer, numLayers)
	for i := uint16(0); i < numLayers; i++ {
		valueLen, err := r.uint32_(fmt.Sprintf("layer %d value length", i))
		if err != nil {
			return nil, err
		}
		if valueLen > MaxFieldSize {
			return nil, fmt.Errorf("wire: layer %d value too long (%d)", i, valueLen)
		}
		value, err := r.bytes(int(valueLen), fmt.Sprintf("layer %d value", i))
		if err != nil {
			return nil, err
		}
		ctLen, err := r.uint32_(fmt.Sprintf("layer %d ciphertext length", i))
		if err != nil {
			return nil, err
		}
		if ctLen > MaxFieldSize {
			return nil, fmt.Errorf("wire: layer %d ciphertext too long (%d)", i, ctLen)
		}
		ct, err := r.bytes(int(ctLen), fmt.Sprintf("layer %d ciphertext", i))
		if err != nil {
			return nil, err
		}
		layers[i] = obft.EncryptedLayer{
			Value:      obft.Value(value),
			Ciphertext: ct,
		}
	}
	if r.remaining() != 0 {
		return nil, fmt.Errorf("wire: %d trailing bytes after onion", r.remaining())
	}
	return &obft.Onion{
		OperatorID: obft.OperatorID(opID),
		Height:     obft.Height(height),
		Layers:     layers,
	}, nil
}

// EncodeNR serializes an NR message (KindNR payload).
//
// Format (version 0x01):
//
//	[1] version
//	[8] OperatorID    (uint64 big-endian)
//	[8] Height        (uint64 big-endian)
//	[2] partial count (uint16 big-endian)
//	for each partial:
//	  [4] Layer        (uint32 big-endian)
//	  [4] sig length   (uint32 big-endian)
//	  [sig bytes]
func EncodeNR(nr *obft.NR) ([]byte, error) {
	if nr == nil {
		return nil, errors.New("wire: nil NR")
	}
	if len(nr.Partials) > MaxLayers {
		return nil, fmt.Errorf("wire: NR has %d partials (max %d)", len(nr.Partials), MaxLayers)
	}

	size := 1 + 8 + 8 + 2
	for _, p := range nr.Partials {
		size += 4 + 4 + len(p.PartialSig)
	}
	out := make([]byte, 0, size)
	out = append(out, NRVersionV1)
	out = appendUint64(out, uint64(nr.OperatorID))
	out = appendUint64(out, uint64(nr.Height))
	out = appendUint16(out, uint16(len(nr.Partials))) //nolint:gosec // bounds-checked
	for _, p := range nr.Partials {
		if p.Layer < 0 {
			return nil, fmt.Errorf("wire: NR partial has negative layer %d", p.Layer)
		}
		if len(p.PartialSig) > MaxFieldSize {
			return nil, fmt.Errorf("wire: NR partial sig too long (%d)", len(p.PartialSig))
		}
		out = appendUint32(out, uint32(p.Layer))            //nolint:gosec // bounds-checked
		out = appendUint32(out, uint32(len(p.PartialSig))) //nolint:gosec // bounds-checked
		out = append(out, p.PartialSig...)
	}
	return out, nil
}

// DecodeNR parses bytes produced by EncodeNR.
func DecodeNR(data []byte) (*obft.NR, error) {
	r := newReader(data)
	version, err := r.byte_("version")
	if err != nil {
		return nil, err
	}
	if version != NRVersionV1 {
		return nil, fmt.Errorf("wire: unsupported NR version 0x%02x", version)
	}
	opID, err := r.uint64_("operator id")
	if err != nil {
		return nil, err
	}
	height, err := r.uint64_("height")
	if err != nil {
		return nil, err
	}
	count, err := r.uint16_("partial count")
	if err != nil {
		return nil, err
	}
	if int(count) > MaxLayers {
		return nil, fmt.Errorf("wire: NR declares %d partials (max %d)", count, MaxLayers)
	}
	partials := make([]obft.NRPartial, count)
	for i := uint16(0); i < count; i++ {
		layer, err := r.uint32_(fmt.Sprintf("partial %d layer", i))
		if err != nil {
			return nil, err
		}
		sigLen, err := r.uint32_(fmt.Sprintf("partial %d sig length", i))
		if err != nil {
			return nil, err
		}
		if sigLen > MaxFieldSize {
			return nil, fmt.Errorf("wire: NR partial %d sig too long (%d)", i, sigLen)
		}
		sig, err := r.bytes(int(sigLen), fmt.Sprintf("partial %d sig", i))
		if err != nil {
			return nil, err
		}
		partials[i] = obft.NRPartial{
			Layer:      int(layer),
			PartialSig: obft.Signature(sig),
		}
	}
	if r.remaining() != 0 {
		return nil, fmt.Errorf("wire: %d trailing bytes after NR", r.remaining())
	}
	return &obft.NR{
		OperatorID: obft.OperatorID(opID),
		Height:     obft.Height(height),
		Partials:   partials,
	}, nil
}

// EncodeCertificate serializes a Certificate (KindCertificate payload).
//
// Format (version 0x01):
//
//	[1] version
//	[8] Height          (uint64 big-endian)
//	[4] Value length    (uint32 big-endian)
//	[Value bytes]
//	[4] Signature length (uint32 big-endian)
//	[Signature bytes]
func EncodeCertificate(c *obft.Certificate) ([]byte, error) {
	if c == nil {
		return nil, errors.New("wire: nil certificate")
	}
	if len(c.Value) > MaxFieldSize {
		return nil, fmt.Errorf("wire: certificate value too long (%d)", len(c.Value))
	}
	if len(c.Signature) > MaxFieldSize {
		return nil, fmt.Errorf("wire: certificate signature too long (%d)", len(c.Signature))
	}

	size := 1 + 8 + 4 + len(c.Value) + 4 + len(c.Signature)
	out := make([]byte, 0, size)
	out = append(out, CertificateVersionV1)
	out = appendUint64(out, uint64(c.Height))
	out = appendUint32(out, uint32(len(c.Value)))      //nolint:gosec // bounds-checked
	out = append(out, c.Value...)                      //
	out = appendUint32(out, uint32(len(c.Signature))) //nolint:gosec // bounds-checked
	out = append(out, c.Signature...)
	return out, nil
}

// DecodeCertificate parses bytes produced by EncodeCertificate.
func DecodeCertificate(data []byte) (*obft.Certificate, error) {
	r := newReader(data)
	version, err := r.byte_("version")
	if err != nil {
		return nil, err
	}
	if version != CertificateVersionV1 {
		return nil, fmt.Errorf("wire: unsupported certificate version 0x%02x", version)
	}
	height, err := r.uint64_("height")
	if err != nil {
		return nil, err
	}
	valueLen, err := r.uint32_("value length")
	if err != nil {
		return nil, err
	}
	if valueLen > MaxFieldSize {
		return nil, fmt.Errorf("wire: certificate value too long (%d)", valueLen)
	}
	value, err := r.bytes(int(valueLen), "value")
	if err != nil {
		return nil, err
	}
	sigLen, err := r.uint32_("signature length")
	if err != nil {
		return nil, err
	}
	if sigLen > MaxFieldSize {
		return nil, fmt.Errorf("wire: certificate signature too long (%d)", sigLen)
	}
	sig, err := r.bytes(int(sigLen), "signature")
	if err != nil {
		return nil, err
	}
	if r.remaining() != 0 {
		return nil, fmt.Errorf("wire: %d trailing bytes after certificate", r.remaining())
	}
	return &obft.Certificate{
		Height:    obft.Height(height),
		Value:     obft.Value(value),
		Signature: obft.Signature(sig),
	}, nil
}

// ---- internal byte readers / writers ------------------------------------

func appendUint16(b []byte, v uint16) []byte {
	var buf [2]byte
	binary.BigEndian.PutUint16(buf[:], v)
	return append(b, buf[:]...)
}

func appendUint32(b []byte, v uint32) []byte {
	var buf [4]byte
	binary.BigEndian.PutUint32(buf[:], v)
	return append(b, buf[:]...)
}

func appendUint64(b []byte, v uint64) []byte {
	var buf [8]byte
	binary.BigEndian.PutUint64(buf[:], v)
	return append(b, buf[:]...)
}

type reader struct {
	data []byte
	pos  int
}

func newReader(data []byte) *reader { return &reader{data: data} }

func (r *reader) remaining() int { return len(r.data) - r.pos }

func (r *reader) byte_(name string) (byte, error) {
	if r.remaining() < 1 {
		return 0, fmt.Errorf("wire: truncated reading %s", name)
	}
	b := r.data[r.pos]
	r.pos++
	return b, nil
}

func (r *reader) uint16_(name string) (uint16, error) {
	if r.remaining() < 2 {
		return 0, fmt.Errorf("wire: truncated reading %s", name)
	}
	v := binary.BigEndian.Uint16(r.data[r.pos : r.pos+2])
	r.pos += 2
	return v, nil
}

func (r *reader) uint32_(name string) (uint32, error) {
	if r.remaining() < 4 {
		return 0, fmt.Errorf("wire: truncated reading %s", name)
	}
	v := binary.BigEndian.Uint32(r.data[r.pos : r.pos+4])
	r.pos += 4
	return v, nil
}

func (r *reader) uint64_(name string) (uint64, error) {
	if r.remaining() < 8 {
		return 0, fmt.Errorf("wire: truncated reading %s", name)
	}
	v := binary.BigEndian.Uint64(r.data[r.pos : r.pos+8])
	r.pos += 8
	return v, nil
}

func (r *reader) bytes(n int, name string) ([]byte, error) {
	if n < 0 {
		return nil, fmt.Errorf("wire: negative length reading %s", name)
	}
	if r.remaining() < n {
		return nil, fmt.Errorf("wire: truncated reading %s (need %d, have %d)", name, n, r.remaining())
	}
	// Defensive copy so the returned slice isn't aliased to the input buffer.
	out := make([]byte, n)
	copy(out, r.data[r.pos:r.pos+n])
	r.pos += n
	return out, nil
}
