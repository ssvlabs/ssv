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

	base "github.com/ssvlabs/ssv/protocol/v2/obft/base"
)

// Wire format versions. Bumped when the on-the-wire layout changes
// incompatibly. Encoders write the current version; decoders accept only
// versions they understand.
//
// Cluster-cutover policy: operators MUST be rolled in lockstep across
// any wire-version bump. Mixed-version clusters will see all cross-
// version messages fail at the version-byte check inside the relevant
// Decode* function, manifesting as silent quorum starvation (the
// upgraded ops reject the legacy ops' bundles/commits as malformed,
// and vice versa). There is no cross-version compatibility layer; the
// decoder accepts exactly one version per kind. Pre-deployment
// verification SHOULD ensure all cluster members run the same protocol
// minor version before a version-bumping release is rolled out.
//
// Mirrors the equivalent migration policy in
// `protocol/v2/obft/twoab/wire/wire.go`.
const (
	Phase1BundleVersionV1 byte = 0x01
	CommitVersionV1       byte = 0x01
	CertificateVersionV1  byte = 0x01
)

// ProtocolTag is the fixed 8-byte literal stamped into every inner OBFT
// message's signed bytes. Decoders reject messages with a mismatching tag.
// Per spec §Auth envelope: binds the protocol identity into the bytes that
// the outer SSV signature covers, so an attacker cannot reinterpret an
// OBFT-v1 message as a future OBFT-vN protocol or vice versa.
var ProtocolTag = [8]byte{'O', 'B', 'F', 'T', '-', 'v', '1', 0}

// Inner-kind tag: each message type stamps its own one-byte kind into the
// inner signed bytes (defense-in-depth on top of the outer wire envelope's
// kind byte). Mismatch with the decoder's expected kind is a structural
// error.
const (
	innerKindPhase1Bundle byte = 0x01
	innerKindCommit       byte = 0x02
	innerKindCertificate  byte = 0x03
)

// MaxLayers caps the number of layers a Commit can declare on the wire.
// Real OBFT configs use K ≤ n ≤ 13 in SSV; anything past 32 is almost
// certainly malformed/malicious.
const MaxLayers = 32

// Per-field length caps on length-prefixed payloads. Each cap is tight to
// the realistic size of its field type, with a healthy multiplicative
// margin for future protocol evolution. The wire decoder rejects any field
// length exceeding its cap before allocating the slice, bounding the
// unbounded-allocation surface a malformed message can exercise.
//
// Choosing per-field bounds (rather than one global MaxFieldSize) tightens
// the realistic upper bound on a Commit body's retained size — relevant
// because a single byzantine's FIRST distinct Commit is deep-copied into
// peerFirstCommit until Rule 3 fires on the second distinct emission, and
// the protocol layer retains up to MaxRetainedPerOpLayer × n × layers of
// onion entries during a slot.
const (
	// MaxValueSize caps proposer-duty candidate values (Phase1Bundle.Value,
	// EncryptedLayer.Value, Certificate.Value). Real beacon-block-V3 with
	// EIP-4844 blob commitments lands ~1–2 KB; future scaling (PeerDAS,
	// larger blocks) may grow this. 1 MiB gives ~500× margin while still
	// bounding total commit retention to single-digit MB per byzantine.
	MaxValueSize = 1 * 1024 * 1024

	// MaxSignatureSize caps BLS partial / aggregate signatures
	// (Phase1Bundle.LeaderSigma, NRPartial.PartialSig, LeaderSigmaWitness.Sigma,
	// Certificate.Signature). Real BLS12-381 signatures are 96 B and
	// partial-sig aggregates the same. 1 KiB is 10× margin against any
	// conceivable future signature scheme variant.
	MaxSignatureSize = 1 * 1024

	// MaxCiphertextSize caps IBE-wrapped σ partials in commit-layer onions
	// (EncryptedLayer.Ciphertext). Inner plaintext is a ~96 B BLS partial;
	// chained-IBE wrapping at layer k applies k encryptions, each adding
	// constant overhead (~300 B for current IBE schemes). At K ≤ MaxLayers
	// the worst case is ≈ 96 + 32 × 300 ≈ 10 KiB; 64 KiB gives ~6× margin.
	MaxCiphertextSize = 64 * 1024
)

// MaxFieldSize is the coarse upper bound covering any single length-prefixed
// field — equal to MaxValueSize because Value is the largest field type.
// Retained for backward compatibility with external test helpers that use
// it as a sanity ceiling on the realistic field space.
const MaxFieldSize = MaxValueSize

// EncodePhase1Bundle serializes a Phase-1 bundle.
//
// Format (version 0x01):
//
//	[1]  version
//	[8]  ProtocolTag   "OBFT-v1\0"
//	[1]  inner kind    = innerKindPhase1Bundle
//	[32] ClusterID
//	[8]  OperatorID    (uint64 big-endian)
//	[8]  Height        (uint64 big-endian)
//	[4]  Layer         (uint32 big-endian)
//	[4]  Value length  (uint32 big-endian)
//	[Value bytes]
//	[4]  LeaderSigma length (uint32 big-endian)
//	[LeaderSigma bytes]
func EncodePhase1Bundle(b *base.Phase1Bundle) ([]byte, error) {
	if b == nil {
		return nil, errors.New("wire: nil phase-1 bundle")
	}
	if b.Layer < 0 {
		return nil, fmt.Errorf("wire: phase-1 bundle has negative layer %d", b.Layer)
	}
	if len(b.Value) > MaxValueSize {
		return nil, fmt.Errorf("wire: phase-1 bundle value too long (%d)", len(b.Value))
	}
	if len(b.LeaderSigma) > MaxSignatureSize {
		return nil, fmt.Errorf("wire: phase-1 bundle LeaderSigma too long (%d)", len(b.LeaderSigma))
	}

	size := 1 + 8 + 1 + 32 + 8 + 8 + 4 + 4 + len(b.Value) + 4 + len(b.LeaderSigma)
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
	out = appendUint32(out, uint32(len(b.LeaderSigma))) //nolint:gosec // bounds-checked
	out = append(out, b.LeaderSigma...)
	return out, nil
}

// DecodePhase1Bundle parses bytes produced by EncodePhase1Bundle.
func DecodePhase1Bundle(data []byte) (*base.Phase1Bundle, error) {
	r := newReader(data)
	version, err := r.byte_("version")
	if err != nil {
		return nil, err
	}
	if version != Phase1BundleVersionV1 {
		return nil, fmt.Errorf("wire: unsupported phase-1 bundle version 0x%02x", version)
	}
	if err := r.expectProtocolTag(); err != nil {
		return nil, err
	}
	if err := r.expectInnerKind(innerKindPhase1Bundle, "phase-1 bundle"); err != nil {
		return nil, err
	}
	clusterID, err := r.cluster_("cluster id")
	if err != nil {
		return nil, err
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
	// Layer is uint32 on the wire but no real OBFT config has K > MaxLayers.
	// Reject early so a malformed message doesn't reach protocol validation.
	if layer >= MaxLayers {
		return nil, fmt.Errorf("wire: phase-1 bundle layer %d exceeds MaxLayers %d", layer, MaxLayers)
	}
	valueLen, err := r.uint32_("value length")
	if err != nil {
		return nil, err
	}
	if valueLen > MaxValueSize {
		return nil, fmt.Errorf("wire: phase-1 value too long (%d)", valueLen)
	}
	value, err := r.bytes(int(valueLen), "value")
	if err != nil {
		return nil, err
	}
	sigLen, err := r.uint32_("LeaderSigma length")
	if err != nil {
		return nil, err
	}
	if sigLen > MaxSignatureSize {
		return nil, fmt.Errorf("wire: phase-1 LeaderSigma too long (%d)", sigLen)
	}
	sig, err := r.bytes(int(sigLen), "LeaderSigma")
	if err != nil {
		return nil, err
	}
	if r.remaining() != 0 {
		return nil, fmt.Errorf("wire: %d trailing bytes after phase-1 bundle", r.remaining())
	}
	return &base.Phase1Bundle{
		ClusterID:   clusterID,
		OperatorID:  base.OperatorID(opID),
		Height:      base.Height(height),
		Layer:       int(layer),
		Value:       base.Value(value),
		LeaderSigma: base.Signature(sig),
	}, nil
}

// MaxWitnesses caps the number of leader-σ_L^V witnesses a Commit can carry.
// Derived from MaxLayers × base.MaxRetainedPerOpLayer: an honest commit
// witnesses up to MaxRetainedPerOpLayer distinct V's per (layer, leader)
// across MaxLayers layers.
const MaxWitnesses = MaxLayers * base.MaxRetainedPerOpLayer

// EncodeCommit serializes a Commit (KindCommit payload).
//
// Format (version 0x01):
//
//	[1]  version
//	[8]  ProtocolTag      "OBFT-v1\0"
//	[1]  inner kind       = innerKindCommit
//	[32] ClusterID
//	[8]  OperatorID         (uint64 big-endian)
//	[8]  Height             (uint64 big-endian)
//	[2]  number of layers   (uint16 big-endian)
//	for each layer:
//	  [4] value length      (uint32 big-endian)
//	  [value bytes]
//	  [4] ciphertext length (uint32 big-endian)
//	  [ciphertext bytes]
//	[2] number of NR partials (uint16 big-endian)
//	for each NR partial:
//	  [4] Layer        (uint32 big-endian)
//	  [4] sig length   (uint32 big-endian)
//	  [sig bytes]
//	[2] number of witnesses (uint16 big-endian)
//	for each witness:
//	  [4]  Layer            (uint32 big-endian)
//	  [8]  Leader OperatorID (uint64 big-endian)
//	  [32] ValueRoot        (sha256(V), fixed-length)
//	  [4]  Sigma length    (uint32 big-endian)
//	  [Sigma bytes]
func EncodeCommit(c *base.Commit) ([]byte, error) {
	if c == nil {
		return nil, errors.New("wire: nil commit")
	}
	if len(c.Layers) > MaxLayers {
		return nil, fmt.Errorf("wire: commit has %d σ layers (max %d)", len(c.Layers), MaxLayers)
	}
	if len(c.NRPartials) > MaxLayers {
		return nil, fmt.Errorf("wire: commit has %d NR partials (max %d)", len(c.NRPartials), MaxLayers)
	}
	if len(c.Witnesses) > MaxWitnesses {
		return nil, fmt.Errorf("wire: commit has %d witnesses (max %d)", len(c.Witnesses), MaxWitnesses)
	}

	size := 1 + 8 + 1 + 32 + 8 + 8 + 2
	for _, el := range c.Layers {
		size += 4 + len(el.Value) + 4 + len(el.Ciphertext)
	}
	size += 2
	for _, p := range c.NRPartials {
		size += 4 + 4 + len(p.PartialSig)
	}
	size += 2
	for _, w := range c.Witnesses {
		// Layer (4) + Leader (8) + ValueRoot (32) + Sigma length (4) + Sigma.
		size += 4 + 8 + 32 + 4 + len(w.Sigma)
	}
	out := make([]byte, 0, size)
	out = append(out, CommitVersionV1)
	out = append(out, ProtocolTag[:]...)
	out = append(out, innerKindCommit)
	out = append(out, c.ClusterID[:]...)
	out = appendUint64(out, uint64(c.OperatorID))
	out = appendUint64(out, uint64(c.Height))
	out = appendUint16(out, uint16(len(c.Layers))) //nolint:gosec // MaxLayers <= uint16 max
	for i, el := range c.Layers {
		if len(el.Value) > MaxValueSize {
			return nil, fmt.Errorf("wire: commit layer %d value too long (%d)", i, len(el.Value))
		}
		if len(el.Ciphertext) > MaxCiphertextSize {
			return nil, fmt.Errorf("wire: commit layer %d ciphertext too long (%d)", i, len(el.Ciphertext))
		}
		out = appendUint32(out, uint32(len(el.Value)))      //nolint:gosec // bounds-checked
		out = append(out, el.Value...)                      //
		out = appendUint32(out, uint32(len(el.Ciphertext))) //nolint:gosec // bounds-checked
		out = append(out, el.Ciphertext...)
	}
	out = appendUint16(out, uint16(len(c.NRPartials))) //nolint:gosec // bounds-checked
	for _, p := range c.NRPartials {
		if p.Layer < 0 {
			return nil, fmt.Errorf("wire: commit NR partial has negative layer %d", p.Layer)
		}
		if len(p.PartialSig) > MaxSignatureSize {
			return nil, fmt.Errorf("wire: commit NR partial sig too long (%d)", len(p.PartialSig))
		}
		out = appendUint32(out, uint32(p.Layer))           //nolint:gosec // bounds-checked
		out = appendUint32(out, uint32(len(p.PartialSig))) //nolint:gosec // bounds-checked
		out = append(out, p.PartialSig...)
	}
	out = appendUint16(out, uint16(len(c.Witnesses))) //nolint:gosec // bounds-checked
	for i, w := range c.Witnesses {
		if w.Layer < 0 {
			return nil, fmt.Errorf("wire: commit witness %d has negative layer %d", i, w.Layer)
		}
		if len(w.Sigma) > MaxSignatureSize {
			return nil, fmt.Errorf("wire: commit witness %d leader sigma too long (%d)", i, len(w.Sigma))
		}
		out = appendUint32(out, uint32(w.Layer))      //nolint:gosec // bounds-checked
		out = appendUint64(out, uint64(w.Leader))     //
		out = append(out, w.ValueRoot[:]...)          // fixed 32 bytes
		out = appendUint32(out, uint32(len(w.Sigma))) //nolint:gosec // bounds-checked
		out = append(out, w.Sigma...)
	}
	return out, nil
}

// DecodeCommit parses bytes produced by EncodeCommit.
func DecodeCommit(data []byte) (*base.Commit, error) {
	r := newReader(data)
	version, err := r.byte_("version")
	if err != nil {
		return nil, err
	}
	if version != CommitVersionV1 {
		return nil, fmt.Errorf("wire: unsupported commit version 0x%02x", version)
	}
	if err := r.expectProtocolTag(); err != nil {
		return nil, err
	}
	if err := r.expectInnerKind(innerKindCommit, "commit"); err != nil {
		return nil, err
	}
	clusterID, err := r.cluster_("cluster id")
	if err != nil {
		return nil, err
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
		return nil, fmt.Errorf("wire: commit declares %d σ layers (max %d)", numLayers, MaxLayers)
	}
	layers := make([]base.EncryptedLayer, numLayers)
	for i := uint16(0); i < numLayers; i++ {
		valueLen, err := r.uint32_(fmt.Sprintf("layer %d value length", i))
		if err != nil {
			return nil, err
		}
		if valueLen > MaxValueSize {
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
		if ctLen > MaxCiphertextSize {
			return nil, fmt.Errorf("wire: layer %d ciphertext too long (%d)", i, ctLen)
		}
		ct, err := r.bytes(int(ctLen), fmt.Sprintf("layer %d ciphertext", i))
		if err != nil {
			return nil, err
		}
		layers[i] = base.EncryptedLayer{
			Value:      base.Value(value),
			Ciphertext: ct,
		}
	}
	nrCount, err := r.uint16_("NR partial count")
	if err != nil {
		return nil, err
	}
	if int(nrCount) > MaxLayers {
		return nil, fmt.Errorf("wire: commit declares %d NR partials (max %d)", nrCount, MaxLayers)
	}
	partials := make([]base.NRPartial, nrCount)
	for i := uint16(0); i < nrCount; i++ {
		layer, err := r.uint32_(fmt.Sprintf("NR partial %d layer", i))
		if err != nil {
			return nil, err
		}
		if layer >= MaxLayers {
			return nil, fmt.Errorf("wire: NR partial %d layer %d exceeds MaxLayers %d", i, layer, MaxLayers)
		}
		sigLen, err := r.uint32_(fmt.Sprintf("NR partial %d sig length", i))
		if err != nil {
			return nil, err
		}
		if sigLen > MaxSignatureSize {
			return nil, fmt.Errorf("wire: NR partial %d sig too long (%d)", i, sigLen)
		}
		sig, err := r.bytes(int(sigLen), fmt.Sprintf("NR partial %d sig", i))
		if err != nil {
			return nil, err
		}
		partials[i] = base.NRPartial{
			Layer:      int(layer),
			PartialSig: base.Signature(sig),
		}
	}
	witnessCount, err := r.uint16_("witness count")
	if err != nil {
		return nil, err
	}
	if int(witnessCount) > MaxWitnesses {
		return nil, fmt.Errorf("wire: commit declares %d witnesses (max %d)", witnessCount, MaxWitnesses)
	}
	witnesses := make([]base.LeaderSigmaWitness, witnessCount)
	for i := uint16(0); i < witnessCount; i++ {
		layer, err := r.uint32_(fmt.Sprintf("witness %d layer", i))
		if err != nil {
			return nil, err
		}
		if layer >= MaxLayers {
			return nil, fmt.Errorf("wire: witness %d layer %d exceeds MaxLayers %d", i, layer, MaxLayers)
		}
		leader, err := r.uint64_(fmt.Sprintf("witness %d leader", i))
		if err != nil {
			return nil, err
		}
		valueRootBytes, err := r.bytes(32, fmt.Sprintf("witness %d valueRoot", i))
		if err != nil {
			return nil, err
		}
		sigLen, err := r.uint32_(fmt.Sprintf("witness %d leader sigma length", i))
		if err != nil {
			return nil, err
		}
		if sigLen > MaxSignatureSize {
			return nil, fmt.Errorf("wire: witness %d leader sigma too long (%d)", i, sigLen)
		}
		sig, err := r.bytes(int(sigLen), fmt.Sprintf("witness %d leader sigma", i))
		if err != nil {
			return nil, err
		}
		var valueRoot [32]byte
		copy(valueRoot[:], valueRootBytes)
		witnesses[i] = base.LeaderSigmaWitness{
			Layer:     int(layer),
			Leader:    base.OperatorID(leader),
			ValueRoot: valueRoot,
			Sigma:     base.Signature(sig),
		}
	}
	if r.remaining() != 0 {
		return nil, fmt.Errorf("wire: %d trailing bytes after commit", r.remaining())
	}
	return &base.Commit{
		ClusterID:  clusterID,
		OperatorID: base.OperatorID(opID),
		Height:     base.Height(height),
		Layers:     layers,
		NRPartials: partials,
		Witnesses:  witnesses,
	}, nil
}

// EncodeCertificate serializes a Certificate (KindCertificate payload).
//
// Format (version 0x01):
//
//	[1]  version
//	[8]  ProtocolTag      "OBFT-v1\0"
//	[1]  inner kind       = innerKindCertificate
//	[32] ClusterID
//	[8]  Height            (uint64 big-endian)
//	[4]  Value length      (uint32 big-endian)
//	[Value bytes]
//	[4]  Signature length  (uint32 big-endian)
//	[Signature bytes]
func EncodeCertificate(c *base.Certificate) ([]byte, error) {
	if c == nil {
		return nil, errors.New("wire: nil certificate")
	}
	if len(c.Value) > MaxValueSize {
		return nil, fmt.Errorf("wire: certificate value too long (%d)", len(c.Value))
	}
	if len(c.Signature) > MaxSignatureSize {
		return nil, fmt.Errorf("wire: certificate signature too long (%d)", len(c.Signature))
	}

	size := 1 + 8 + 1 + 32 + 8 + 4 + len(c.Value) + 4 + len(c.Signature)
	out := make([]byte, 0, size)
	out = append(out, CertificateVersionV1)
	out = append(out, ProtocolTag[:]...)
	out = append(out, innerKindCertificate)
	out = append(out, c.ClusterID[:]...)
	out = appendUint64(out, uint64(c.Height))
	out = appendUint32(out, uint32(len(c.Value)))     //nolint:gosec // bounds-checked
	out = append(out, c.Value...)                     //
	out = appendUint32(out, uint32(len(c.Signature))) //nolint:gosec // bounds-checked
	out = append(out, c.Signature...)
	return out, nil
}

// DecodeCertificate parses bytes produced by EncodeCertificate.
func DecodeCertificate(data []byte) (*base.Certificate, error) {
	r := newReader(data)
	version, err := r.byte_("version")
	if err != nil {
		return nil, err
	}
	if version != CertificateVersionV1 {
		return nil, fmt.Errorf("wire: unsupported certificate version 0x%02x", version)
	}
	if err := r.expectProtocolTag(); err != nil {
		return nil, err
	}
	if err := r.expectInnerKind(innerKindCertificate, "certificate"); err != nil {
		return nil, err
	}
	clusterID, err := r.cluster_("cluster id")
	if err != nil {
		return nil, err
	}
	height, err := r.uint64_("height")
	if err != nil {
		return nil, err
	}
	valueLen, err := r.uint32_("value length")
	if err != nil {
		return nil, err
	}
	if valueLen > MaxValueSize {
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
	if sigLen > MaxSignatureSize {
		return nil, fmt.Errorf("wire: certificate signature too long (%d)", sigLen)
	}
	sig, err := r.bytes(int(sigLen), "signature")
	if err != nil {
		return nil, err
	}
	if r.remaining() != 0 {
		return nil, fmt.Errorf("wire: %d trailing bytes after certificate", r.remaining())
	}
	return &base.Certificate{
		ClusterID: clusterID,
		Height:    base.Height(height),
		Value:     base.Value(value),
		Signature: base.Signature(sig),
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

// expectProtocolTag reads 8 bytes and checks they match ProtocolTag.
func (r *reader) expectProtocolTag() error {
	tag, err := r.bytes(8, "protocol tag")
	if err != nil {
		return err
	}
	for i, b := range ProtocolTag {
		if tag[i] != b {
			return fmt.Errorf("wire: protocol tag mismatch (got %q, want %q)", tag, ProtocolTag[:])
		}
	}
	return nil
}

// expectInnerKind reads 1 byte and checks it equals `want`.
func (r *reader) expectInnerKind(want byte, label string) error {
	got, err := r.byte_("inner kind for " + label)
	if err != nil {
		return err
	}
	if got != want {
		return fmt.Errorf("wire: %s inner kind 0x%02x != expected 0x%02x", label, got, want)
	}
	return nil
}

func (r *reader) cluster_(name string) ([32]byte, error) {
	var out [32]byte
	if r.remaining() < 32 {
		return out, fmt.Errorf("wire: truncated reading %s", name)
	}
	copy(out[:], r.data[r.pos:r.pos+32])
	r.pos += 32
	return out, nil
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
