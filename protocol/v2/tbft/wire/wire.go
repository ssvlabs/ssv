// Package wire provides binary serialization for TBFT messages that flow
// over the network. The encoding is intentionally simple (length-prefixed
// fields, big-endian integers, version byte) — not SSZ. SSZ migration is
// possible later if Eth2 ecosystem alignment becomes useful; for now,
// simplicity wins.
//
// The wire format is independent of any specific p2p envelope: the SSV
// adapter wraps the bytes produced here in a SignedSSVMessage (or whatever
// the runtime uses). This package only handles the (un)marshalling of
// TBFT message bodies.
package wire

import (
	"encoding/binary"
	"errors"
	"fmt"

	"github.com/ssvlabs/ssv/protocol/v2/tbft"
)

// Wire format versions. Bumped when the on-the-wire layout changes
// incompatibly. Encoders write the current version; decoders must accept
// any version they understand.
const (
	OnionVersionV1      byte = 0x01
	NonReceiptVersionV1 byte = 0x01
	CandidateVersionV1  byte = 0x01
)

// MaxLayers is a defensive upper bound on the number of layers an onion
// can declare on the wire. Real configs use K ≤ ~5; anything larger is
// almost certainly a malformed/malicious message and should be rejected
// before allocation. Keeps decoders allocation-bounded.
const MaxLayers = 32

// MaxFieldSize is a defensive upper bound on individual length-prefixed
// fields (tags, values, ciphertexts, signatures). 16 MiB is far above
// realistic SSV proposer-duty values (~1 KB blinded blocks, ~96 B
// signatures) but still safe against unbounded allocation from a
// malformed message.
const MaxFieldSize = 16 * 1024 * 1024

// EncodeOnion serialises an Onion to bytes.
//
// Format (version 0x01):
//
//	[1] version
//	[8] OperatorID         (uint64 big-endian)
//	[8] Height             (uint64 big-endian)
//	[2] number of layers   (uint16 big-endian)
//	for each layer:
//	  [2] tag length        (uint16 big-endian)
//	  [tag bytes]
//	  [4] value length      (uint32 big-endian)
//	  [value bytes]
//	  [4] ciphertext length (uint32 big-endian)
//	  [ciphertext bytes]
func EncodeOnion(o *tbft.Onion) ([]byte, error) {
	if o == nil {
		return nil, errors.New("wire: nil onion")
	}
	if len(o.Layers) > MaxLayers {
		return nil, fmt.Errorf("wire: onion has %d layers (max %d)", len(o.Layers), MaxLayers)
	}

	// Pre-compute size for a single allocation.
	size := 1 + 8 + 8 + 2
	for _, el := range o.Layers {
		size += 2 + len(el.Tag) + 4 + len(el.Value) + 4 + len(el.Ciphertext)
	}

	out := make([]byte, 0, size)
	out = append(out, OnionVersionV1)
	out = appendUint64(out, uint64(o.OperatorID))
	out = appendUint64(out, uint64(o.Height))
	out = appendUint16(out, uint16(len(o.Layers)))
	for i, el := range o.Layers {
		if len(el.Tag) > 0xFFFF {
			return nil, fmt.Errorf("wire: layer %d tag too long (%d)", i, len(el.Tag))
		}
		if len(el.Value) > MaxFieldSize {
			return nil, fmt.Errorf("wire: layer %d value too long (%d)", i, len(el.Value))
		}
		if len(el.Ciphertext) > MaxFieldSize {
			return nil, fmt.Errorf("wire: layer %d ciphertext too long (%d)", i, len(el.Ciphertext))
		}
		out = appendUint16(out, uint16(len(el.Tag)))
		out = append(out, el.Tag...)
		out = appendUint32(out, uint32(len(el.Value)))
		out = append(out, el.Value...)
		out = appendUint32(out, uint32(len(el.Ciphertext)))
		out = append(out, el.Ciphertext...)
	}
	return out, nil
}

// DecodeOnion parses bytes produced by EncodeOnion into an Onion.
func DecodeOnion(data []byte) (*tbft.Onion, error) {
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

	layers := make([]tbft.EncryptedLayer, numLayers)
	for i := uint16(0); i < numLayers; i++ {
		tagLen, err := r.uint16_(fmt.Sprintf("layer %d tag length", i))
		if err != nil {
			return nil, err
		}
		tag, err := r.bytes(int(tagLen), fmt.Sprintf("layer %d tag", i))
		if err != nil {
			return nil, err
		}

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

		layers[i] = tbft.EncryptedLayer{
			Tag:        tag,
			Value:      tbft.Value(value),
			Ciphertext: ct,
		}
	}

	if r.remaining() != 0 {
		return nil, fmt.Errorf("wire: %d trailing bytes after onion", r.remaining())
	}

	return &tbft.Onion{
		OperatorID: tbft.OperatorID(opID),
		Height:     tbft.Height(height),
		Layers:     layers,
	}, nil
}

// EncodeNonReceipt serialises a NonReceiptAttestation.
//
// Format (version 0x01):
//
//	[1] version
//	[8] OperatorID  (uint64 big-endian)
//	[8] Height      (uint64 big-endian)
//	[4] Layer       (uint32 big-endian)
//	[4] sig length  (uint32 big-endian)
//	[sig bytes]
func EncodeNonReceipt(nr *tbft.NonReceiptAttestation) ([]byte, error) {
	if nr == nil {
		return nil, errors.New("wire: nil non-receipt")
	}
	if nr.Layer < 0 {
		return nil, fmt.Errorf("wire: non-receipt has negative layer %d", nr.Layer)
	}
	if len(nr.PartialSig) > MaxFieldSize {
		return nil, fmt.Errorf("wire: non-receipt sig too long (%d)", len(nr.PartialSig))
	}

	size := 1 + 8 + 8 + 4 + 4 + len(nr.PartialSig)
	out := make([]byte, 0, size)
	out = append(out, NonReceiptVersionV1)
	out = appendUint64(out, uint64(nr.OperatorID))
	out = appendUint64(out, uint64(nr.Height))
	out = appendUint32(out, uint32(nr.Layer))
	out = appendUint32(out, uint32(len(nr.PartialSig)))
	out = append(out, nr.PartialSig...)
	return out, nil
}

// EncodeCandidate serialises a CandidateBroadcast.
//
// Format (version 0x01):
//
//	[1] version
//	[8] OperatorID    (uint64 big-endian)
//	[8] Height        (uint64 big-endian)
//	[4] Layer         (uint32 big-endian)
//	[4] Value length  (uint32 big-endian)
//	[Value bytes]
func EncodeCandidate(cb *tbft.CandidateBroadcast) ([]byte, error) {
	if cb == nil {
		return nil, errors.New("wire: nil candidate broadcast")
	}
	if cb.Layer < 0 {
		return nil, fmt.Errorf("wire: candidate broadcast has negative layer %d", cb.Layer)
	}
	if len(cb.Value) > MaxFieldSize {
		return nil, fmt.Errorf("wire: candidate value too long (%d)", len(cb.Value))
	}

	size := 1 + 8 + 8 + 4 + 4 + len(cb.Value)
	out := make([]byte, 0, size)
	out = append(out, CandidateVersionV1)
	out = appendUint64(out, uint64(cb.OperatorID))
	out = appendUint64(out, uint64(cb.Height))
	out = appendUint32(out, uint32(cb.Layer))
	out = appendUint32(out, uint32(len(cb.Value)))
	out = append(out, cb.Value...)
	return out, nil
}

// DecodeCandidate parses bytes produced by EncodeCandidate.
func DecodeCandidate(data []byte) (*tbft.CandidateBroadcast, error) {
	r := newReader(data)

	version, err := r.byte_("version")
	if err != nil {
		return nil, err
	}
	if version != CandidateVersionV1 {
		return nil, fmt.Errorf("wire: unsupported candidate version 0x%02x", version)
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
		return nil, fmt.Errorf("wire: candidate value too long (%d)", valueLen)
	}
	value, err := r.bytes(int(valueLen), "value")
	if err != nil {
		return nil, err
	}
	if r.remaining() != 0 {
		return nil, fmt.Errorf("wire: %d trailing bytes after candidate broadcast", r.remaining())
	}

	return &tbft.CandidateBroadcast{
		OperatorID: tbft.OperatorID(opID),
		Height:     tbft.Height(height),
		Layer:      int(layer),
		Value:      tbft.Value(value),
	}, nil
}

// DecodeNonReceipt parses bytes produced by EncodeNonReceipt.
func DecodeNonReceipt(data []byte) (*tbft.NonReceiptAttestation, error) {
	r := newReader(data)

	version, err := r.byte_("version")
	if err != nil {
		return nil, err
	}
	if version != NonReceiptVersionV1 {
		return nil, fmt.Errorf("wire: unsupported non-receipt version 0x%02x", version)
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
	sigLen, err := r.uint32_("sig length")
	if err != nil {
		return nil, err
	}
	if sigLen > MaxFieldSize {
		return nil, fmt.Errorf("wire: non-receipt sig too long (%d)", sigLen)
	}
	sig, err := r.bytes(int(sigLen), "sig")
	if err != nil {
		return nil, err
	}
	if r.remaining() != 0 {
		return nil, fmt.Errorf("wire: %d trailing bytes after non-receipt", r.remaining())
	}

	return &tbft.NonReceiptAttestation{
		OperatorID: tbft.OperatorID(opID),
		Height:     tbft.Height(height),
		Layer:      int(layer),
		PartialSig: tbft.Signature(sig),
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
