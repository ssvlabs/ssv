// Package wire — encoders/decoders for 2abOBFT message bodies. See
// envelope.go for the wrapping/unwrapping layer.
package wire

import (
	"encoding/binary"
	"errors"
	"fmt"

	"github.com/ssvlabs/ssv/protocol/v2/obft/twoab"
)

// Wire format versions per message kind. Bumped when the on-the-wire
// layout changes incompatibly. Encoders write the current version;
// decoders accept only versions they understand.
const (
	Phase1BundleVersionV1 byte = 0x01
	VerdictVersionV1      byte = 0x01
	Onion2bVersionV1      byte = 0x01
	CertificateVersionV1  byte = 0x01
)

// ProtocolTag is the fixed 16-byte literal stamped into every inner
// 2abOBFT message's signed bytes. Decoders reject messages with a
// mismatching tag.
//
// Per spec §Phase 1 / §Phase 2a / §Phase 2b auth envelope: protocol_tag
// = "2abOBFT-v1". Padded to 16 bytes with NULs to fit a fixed-size field
// (the spec string is 10 chars; bare OBFT uses an 8-byte tag, but 2ab's
// name doesn't fit 8 bytes, so we use 16 — also more headroom for future
// versions like "2abOBFT-v2\0\0\0\0\0\0").
//
// This is the load-bearing domain separation against bare-OBFT envelopes
// — a base-encoded message decoded with twoab/wire.Unwrap fails at the
// ProtocolTag check.
var ProtocolTag = [16]byte{
	'2', 'a', 'b', 'O', 'B', 'F', 'T', '-', 'v', '1',
	0, 0, 0, 0, 0, 0,
}

// Inner-kind tag: each message type stamps its own one-byte kind into the
// inner signed bytes (defense-in-depth on top of the outer envelope's kind
// byte). Mismatch with the decoder's expected kind is a structural error.
const (
	innerKindPhase1Bundle byte = 0x01
	innerKindVerdict      byte = 0x02
	innerKindOnion2b      byte = 0x03
	innerKindCertificate  byte = 0x04
)

// MaxLayers caps the number of layers an Onion2b can declare on the wire.
// Real 2abOBFT configs use K ≤ n ≤ 13 in SSV; anything past 32 is almost
// certainly malformed/malicious.
const MaxLayers = 32

// MaxFieldSize caps individual length-prefixed fields (values, ciphertexts,
// signatures). 16 MiB is far above realistic SSV proposer-duty values
// (~1 KB blinded blocks, ~96 B signatures) but still safe against unbounded
// allocation from a malformed message.
const MaxFieldSize = 16 * 1024 * 1024

// ---------- Phase1Bundle ----------

// EncodePhase1Bundle serializes a Phase-1 bundle.
//
// Format (version 0x01):
//
//	[1]  version
//	[16] ProtocolTag    "2abOBFT-v1" + 6 NULs
//	[1]  inner kind     = innerKindPhase1Bundle
//	[32] ClusterID
//	[8]  OperatorID     (uint64 big-endian)
//	[8]  Height         (uint64 big-endian)
//	[4]  Layer          (uint32 big-endian)
//	[4]  Value length   (uint32 big-endian)
//	[Value bytes]
//
// No σ_V partial — 2abOBFT Variant C; spec §Phase 1.
func EncodePhase1Bundle(b *twoab.Phase1Bundle) ([]byte, error) {
	if b == nil {
		return nil, errors.New("wire: nil phase-1 bundle")
	}
	if b.Layer < 0 {
		return nil, fmt.Errorf("wire: phase-1 bundle has negative layer %d", b.Layer)
	}
	if len(b.Value) > MaxFieldSize {
		return nil, fmt.Errorf("wire: phase-1 bundle value too long (%d)", len(b.Value))
	}

	size := 1 + 16 + 1 + 32 + 8 + 8 + 4 + 4 + len(b.Value)
	out := make([]byte, 0, size)
	out = append(out, Phase1BundleVersionV1)
	out = append(out, ProtocolTag[:]...)
	out = append(out, innerKindPhase1Bundle)
	out = append(out, b.ClusterID[:]...)
	out = appendUint64(out, uint64(b.OperatorID))
	out = appendUint64(out, uint64(b.Height))
	out = appendUint32(out, uint32(b.Layer))      //nolint:gosec // bounds-checked above
	out = appendUint32(out, uint32(len(b.Value))) //nolint:gosec // bounds-checked
	out = append(out, b.Value...)
	return out, nil
}

// DecodePhase1Bundle parses bytes produced by EncodePhase1Bundle.
func DecodePhase1Bundle(data []byte) (*twoab.Phase1Bundle, error) {
	r := newReader(data)
	if err := readVersion(r, Phase1BundleVersionV1, "phase-1 bundle"); err != nil {
		return nil, err
	}
	if err := readProtocolTag(r); err != nil {
		return nil, err
	}
	if err := readInnerKind(r, innerKindPhase1Bundle, "phase-1 bundle"); err != nil {
		return nil, err
	}
	var clusterID [32]byte
	if err := r.readBytes(clusterID[:]); err != nil {
		return nil, fmt.Errorf("wire: phase-1 bundle cluster_id: %w", err)
	}
	opID, err := r.readUint64()
	if err != nil {
		return nil, fmt.Errorf("wire: phase-1 bundle operator_id: %w", err)
	}
	height, err := r.readUint64()
	if err != nil {
		return nil, fmt.Errorf("wire: phase-1 bundle height: %w", err)
	}
	layer, err := r.readUint32()
	if err != nil {
		return nil, fmt.Errorf("wire: phase-1 bundle layer: %w", err)
	}
	if layer > MaxLayers {
		return nil, fmt.Errorf("wire: phase-1 bundle layer %d exceeds MaxLayers %d", layer, MaxLayers)
	}
	value, err := r.readLengthPrefixed("phase-1 bundle value")
	if err != nil {
		return nil, err
	}
	if err := r.requireEOF("phase-1 bundle"); err != nil {
		return nil, err
	}
	return &twoab.Phase1Bundle{
		ClusterID:  clusterID,
		OperatorID: twoab.OperatorID(opID),
		Height:     twoab.Height(height),
		Layer:      int(layer), //nolint:gosec // bounds-checked above
		Value:      twoab.Value(value),
	}, nil
}

// ---------- Verdict ----------

// EncodeVerdict serializes a Phase-2a Verdict envelope.
//
// Format (version 0x01):
//
//	[1]  version
//	[16] ProtocolTag
//	[1]  inner kind   = innerKindVerdict
//	[32] ClusterID
//	[8]  OperatorID
//	[8]  Height
//	[4]  Layer
//	[1]  VerdictKind
//	[32] ValueRoot (sha256(V) for σV; zero bytes for NR / NV)
func EncodeVerdict(v *twoab.Verdict) ([]byte, error) {
	if v == nil {
		return nil, errors.New("wire: nil verdict")
	}
	if v.Layer < 0 {
		return nil, fmt.Errorf("wire: verdict has negative layer %d", v.Layer)
	}
	if v.Kind == twoab.VerdictUnspecified {
		return nil, errors.New("wire: verdict kind is unspecified")
	}

	size := 1 + 16 + 1 + 32 + 8 + 8 + 4 + 1 + 32
	out := make([]byte, 0, size)
	out = append(out, VerdictVersionV1)
	out = append(out, ProtocolTag[:]...)
	out = append(out, innerKindVerdict)
	out = append(out, v.ClusterID[:]...)
	out = appendUint64(out, uint64(v.OperatorID))
	out = appendUint64(out, uint64(v.Height))
	out = appendUint32(out, uint32(v.Layer)) //nolint:gosec // bounds-checked above
	out = append(out, byte(v.Kind))
	out = append(out, v.ValueRoot[:]...)
	return out, nil
}

// DecodeVerdict parses bytes produced by EncodeVerdict.
func DecodeVerdict(data []byte) (*twoab.Verdict, error) {
	r := newReader(data)
	if err := readVersion(r, VerdictVersionV1, "verdict"); err != nil {
		return nil, err
	}
	if err := readProtocolTag(r); err != nil {
		return nil, err
	}
	if err := readInnerKind(r, innerKindVerdict, "verdict"); err != nil {
		return nil, err
	}
	var clusterID [32]byte
	if err := r.readBytes(clusterID[:]); err != nil {
		return nil, fmt.Errorf("wire: verdict cluster_id: %w", err)
	}
	opID, err := r.readUint64()
	if err != nil {
		return nil, fmt.Errorf("wire: verdict operator_id: %w", err)
	}
	height, err := r.readUint64()
	if err != nil {
		return nil, fmt.Errorf("wire: verdict height: %w", err)
	}
	layer, err := r.readUint32()
	if err != nil {
		return nil, fmt.Errorf("wire: verdict layer: %w", err)
	}
	if layer > MaxLayers {
		return nil, fmt.Errorf("wire: verdict layer %d exceeds MaxLayers %d", layer, MaxLayers)
	}
	kindByte, err := r.readByte()
	if err != nil {
		return nil, fmt.Errorf("wire: verdict kind: %w", err)
	}
	kind := twoab.VerdictKind(kindByte)
	if kind == twoab.VerdictUnspecified {
		return nil, errors.New("wire: verdict kind is unspecified")
	}
	if kind != twoab.VerdictSigmaV && kind != twoab.VerdictNR && kind != twoab.VerdictNV {
		return nil, fmt.Errorf("wire: verdict kind 0x%02x is invalid", kindByte)
	}
	var valueRoot [32]byte
	if err := r.readBytes(valueRoot[:]); err != nil {
		return nil, fmt.Errorf("wire: verdict value_root: %w", err)
	}
	if err := r.requireEOF("verdict"); err != nil {
		return nil, err
	}
	return &twoab.Verdict{
		ClusterID:  clusterID,
		OperatorID: twoab.OperatorID(opID),
		Height:     twoab.Height(height),
		Layer:      int(layer), //nolint:gosec // bounds-checked above
		Kind:       kind,
		ValueRoot:  valueRoot,
	}, nil
}

// ---------- Onion2b ----------

// EncodeOnion2b serializes a Phase-2b commit message.
//
// Format (version 0x01):
//
//	[1]  version
//	[16] ProtocolTag
//	[1]  inner kind         = innerKindOnion2b
//	[32] ClusterID
//	[8]  OperatorID
//	[8]  Height
//	[4]  NumLayers          (uint32)
//	for each layer in Layers:
//	    [4] Value length
//	    [Value bytes]
//	    [4] Ciphertext length
//	    [Ciphertext bytes]
//	[4]  NumNRPartials      (uint32)
//	for each NR partial:
//	    [4] Layer (uint32)
//	    [4] PartialSig length
//	    [PartialSig bytes]
func EncodeOnion2b(o *twoab.Onion2b) ([]byte, error) {
	if o == nil {
		return nil, errors.New("wire: nil onion2b")
	}
	if len(o.Layers) > MaxLayers {
		return nil, fmt.Errorf("wire: onion2b has %d layers, max %d", len(o.Layers), MaxLayers)
	}
	if len(o.NRPartials) > MaxLayers {
		return nil, fmt.Errorf("wire: onion2b has %d NR partials, max %d", len(o.NRPartials), MaxLayers)
	}
	// Pre-flight field-size check
	for i, el := range o.Layers {
		if len(el.Value) > MaxFieldSize {
			return nil, fmt.Errorf("wire: onion2b layer %d value too long (%d)", i, len(el.Value))
		}
		if len(el.Ciphertext) > MaxFieldSize {
			return nil, fmt.Errorf("wire: onion2b layer %d ciphertext too long (%d)", i, len(el.Ciphertext))
		}
	}
	for i, p := range o.NRPartials {
		if p.Layer < 0 {
			return nil, fmt.Errorf("wire: onion2b NR partial %d has negative layer", i)
		}
		if len(p.PartialSig) > MaxFieldSize {
			return nil, fmt.Errorf("wire: onion2b NR partial %d sig too long (%d)", i, len(p.PartialSig))
		}
	}

	out := make([]byte, 0, 1+16+1+32+8+8+4+4)
	out = append(out, Onion2bVersionV1)
	out = append(out, ProtocolTag[:]...)
	out = append(out, innerKindOnion2b)
	out = append(out, o.ClusterID[:]...)
	out = appendUint64(out, uint64(o.OperatorID))
	out = appendUint64(out, uint64(o.Height))
	out = appendUint32(out, uint32(len(o.Layers))) //nolint:gosec // bounds-checked above
	for _, el := range o.Layers {
		out = appendUint32(out, uint32(len(el.Value))) //nolint:gosec // bounds-checked
		out = append(out, el.Value...)
		out = appendUint32(out, uint32(len(el.Ciphertext))) //nolint:gosec // bounds-checked
		out = append(out, el.Ciphertext...)
	}
	out = appendUint32(out, uint32(len(o.NRPartials))) //nolint:gosec // bounds-checked above
	for _, p := range o.NRPartials {
		out = appendUint32(out, uint32(p.Layer))           //nolint:gosec // bounds-checked above
		out = appendUint32(out, uint32(len(p.PartialSig))) //nolint:gosec // bounds-checked
		out = append(out, p.PartialSig...)
	}
	return out, nil
}

// DecodeOnion2b parses bytes produced by EncodeOnion2b.
func DecodeOnion2b(data []byte) (*twoab.Onion2b, error) {
	r := newReader(data)
	if err := readVersion(r, Onion2bVersionV1, "onion2b"); err != nil {
		return nil, err
	}
	if err := readProtocolTag(r); err != nil {
		return nil, err
	}
	if err := readInnerKind(r, innerKindOnion2b, "onion2b"); err != nil {
		return nil, err
	}
	var clusterID [32]byte
	if err := r.readBytes(clusterID[:]); err != nil {
		return nil, fmt.Errorf("wire: onion2b cluster_id: %w", err)
	}
	opID, err := r.readUint64()
	if err != nil {
		return nil, fmt.Errorf("wire: onion2b operator_id: %w", err)
	}
	height, err := r.readUint64()
	if err != nil {
		return nil, fmt.Errorf("wire: onion2b height: %w", err)
	}
	numLayers, err := r.readUint32()
	if err != nil {
		return nil, fmt.Errorf("wire: onion2b layer count: %w", err)
	}
	if numLayers > MaxLayers {
		return nil, fmt.Errorf("wire: onion2b layer count %d exceeds MaxLayers %d", numLayers, MaxLayers)
	}
	layers := make([]twoab.EncryptedLayer, numLayers)
	for i := uint32(0); i < numLayers; i++ {
		value, err := r.readLengthPrefixed(fmt.Sprintf("onion2b layer %d value", i))
		if err != nil {
			return nil, err
		}
		ciphertext, err := r.readLengthPrefixed(fmt.Sprintf("onion2b layer %d ciphertext", i))
		if err != nil {
			return nil, err
		}
		layers[i] = twoab.EncryptedLayer{
			Value:      twoab.Value(value),
			Ciphertext: ciphertext,
		}
	}
	numNR, err := r.readUint32()
	if err != nil {
		return nil, fmt.Errorf("wire: onion2b NR partial count: %w", err)
	}
	if numNR > MaxLayers {
		return nil, fmt.Errorf("wire: onion2b NR partial count %d exceeds MaxLayers %d", numNR, MaxLayers)
	}
	nrPartials := make([]twoab.NRPartial, numNR)
	for i := uint32(0); i < numNR; i++ {
		layer, err := r.readUint32()
		if err != nil {
			return nil, fmt.Errorf("wire: onion2b NR partial %d layer: %w", i, err)
		}
		if layer > MaxLayers {
			return nil, fmt.Errorf("wire: onion2b NR partial %d layer %d exceeds MaxLayers %d", i, layer, MaxLayers)
		}
		sig, err := r.readLengthPrefixed(fmt.Sprintf("onion2b NR partial %d sig", i))
		if err != nil {
			return nil, err
		}
		nrPartials[i] = twoab.NRPartial{
			Layer:      int(layer), //nolint:gosec // bounds-checked above
			PartialSig: twoab.Signature(sig),
		}
	}
	if err := r.requireEOF("onion2b"); err != nil {
		return nil, err
	}
	return &twoab.Onion2b{
		ClusterID:  clusterID,
		OperatorID: twoab.OperatorID(opID),
		Height:     twoab.Height(height),
		Layers:     layers,
		NRPartials: nrPartials,
	}, nil
}

// ---------- Certificate ----------

// EncodeCertificate serializes a final-certificate.
//
// Format (version 0x01):
//
//	[1]  version
//	[16] ProtocolTag
//	[1]  inner kind         = innerKindCertificate
//	[32] ClusterID
//	[8]  Height
//	[4]  Value length
//	[Value bytes]
//	[4]  Signature length
//	[Signature bytes]
func EncodeCertificate(c *twoab.Certificate) ([]byte, error) {
	if c == nil {
		return nil, errors.New("wire: nil certificate")
	}
	if len(c.Value) > MaxFieldSize {
		return nil, fmt.Errorf("wire: certificate value too long (%d)", len(c.Value))
	}
	if len(c.Signature) > MaxFieldSize {
		return nil, fmt.Errorf("wire: certificate signature too long (%d)", len(c.Signature))
	}

	size := 1 + 16 + 1 + 32 + 8 + 4 + len(c.Value) + 4 + len(c.Signature)
	out := make([]byte, 0, size)
	out = append(out, CertificateVersionV1)
	out = append(out, ProtocolTag[:]...)
	out = append(out, innerKindCertificate)
	out = append(out, c.ClusterID[:]...)
	out = appendUint64(out, uint64(c.Height))
	out = appendUint32(out, uint32(len(c.Value))) //nolint:gosec // bounds-checked
	out = append(out, c.Value...)
	out = appendUint32(out, uint32(len(c.Signature))) //nolint:gosec // bounds-checked
	out = append(out, c.Signature...)
	return out, nil
}

// DecodeCertificate parses bytes produced by EncodeCertificate.
func DecodeCertificate(data []byte) (*twoab.Certificate, error) {
	r := newReader(data)
	if err := readVersion(r, CertificateVersionV1, "certificate"); err != nil {
		return nil, err
	}
	if err := readProtocolTag(r); err != nil {
		return nil, err
	}
	if err := readInnerKind(r, innerKindCertificate, "certificate"); err != nil {
		return nil, err
	}
	var clusterID [32]byte
	if err := r.readBytes(clusterID[:]); err != nil {
		return nil, fmt.Errorf("wire: certificate cluster_id: %w", err)
	}
	height, err := r.readUint64()
	if err != nil {
		return nil, fmt.Errorf("wire: certificate height: %w", err)
	}
	value, err := r.readLengthPrefixed("certificate value")
	if err != nil {
		return nil, err
	}
	sig, err := r.readLengthPrefixed("certificate signature")
	if err != nil {
		return nil, err
	}
	if err := r.requireEOF("certificate"); err != nil {
		return nil, err
	}
	return &twoab.Certificate{
		ClusterID: clusterID,
		Height:    twoab.Height(height),
		Value:     twoab.Value(value),
		Signature: twoab.Signature(sig),
	}, nil
}

// ---------- Helpers ----------

func appendUint32(out []byte, v uint32) []byte {
	var buf [4]byte
	binary.BigEndian.PutUint32(buf[:], v)
	return append(out, buf[:]...)
}

func appendUint64(out []byte, v uint64) []byte {
	var buf [8]byte
	binary.BigEndian.PutUint64(buf[:], v)
	return append(out, buf[:]...)
}

type reader struct {
	data []byte
	off  int
}

func newReader(data []byte) *reader { return &reader{data: data} }

func (r *reader) remaining() int { return len(r.data) - r.off }

func (r *reader) readByte() (byte, error) {
	if r.remaining() < 1 {
		return 0, errors.New("truncated")
	}
	b := r.data[r.off]
	r.off++
	return b, nil
}

func (r *reader) readBytes(out []byte) error {
	n := len(out)
	if r.remaining() < n {
		return errors.New("truncated")
	}
	copy(out, r.data[r.off:r.off+n])
	r.off += n
	return nil
}

func (r *reader) readUint32() (uint32, error) {
	if r.remaining() < 4 {
		return 0, errors.New("truncated")
	}
	v := binary.BigEndian.Uint32(r.data[r.off : r.off+4])
	r.off += 4
	return v, nil
}

func (r *reader) readUint64() (uint64, error) {
	if r.remaining() < 8 {
		return 0, errors.New("truncated")
	}
	v := binary.BigEndian.Uint64(r.data[r.off : r.off+8])
	r.off += 8
	return v, nil
}

func (r *reader) readLengthPrefixed(field string) ([]byte, error) {
	n, err := r.readUint32()
	if err != nil {
		return nil, fmt.Errorf("wire: %s length: %w", field, err)
	}
	if n > MaxFieldSize {
		return nil, fmt.Errorf("wire: %s length %d exceeds MaxFieldSize %d", field, n, MaxFieldSize)
	}
	if r.remaining() < int(n) {
		return nil, fmt.Errorf("wire: %s body truncated (need %d, have %d)", field, n, r.remaining())
	}
	out := make([]byte, n)
	copy(out, r.data[r.off:r.off+int(n)])
	r.off += int(n)
	return out, nil
}

func (r *reader) requireEOF(field string) error {
	if r.remaining() != 0 {
		return fmt.Errorf("wire: %s has %d trailing bytes", field, r.remaining())
	}
	return nil
}

func readVersion(r *reader, expected byte, field string) error {
	v, err := r.readByte()
	if err != nil {
		return fmt.Errorf("wire: %s version: %w", field, err)
	}
	if v != expected {
		return fmt.Errorf("wire: %s version 0x%02x not supported (expected 0x%02x)", field, v, expected)
	}
	return nil
}

func readProtocolTag(r *reader) error {
	var tag [16]byte
	if err := r.readBytes(tag[:]); err != nil {
		return fmt.Errorf("wire: protocol_tag: %w", err)
	}
	if tag != ProtocolTag {
		return fmt.Errorf("wire: protocol_tag mismatch (got %q, want %q)",
			string(tag[:]), string(ProtocolTag[:]))
	}
	return nil
}

func readInnerKind(r *reader, expected byte, field string) error {
	k, err := r.readByte()
	if err != nil {
		return fmt.Errorf("wire: %s inner kind: %w", field, err)
	}
	if k != expected {
		return fmt.Errorf("wire: %s inner kind 0x%02x mismatch (expected 0x%02x)", field, k, expected)
	}
	return nil
}
