// Package wire provides binary serialization for OBFT messages that flow
// over the network. The encoding is intentionally simple (length-prefixed
// fields, big-endian integers, version byte) — not SSZ.
//
// The wire format is independent of any specific p2p envelope: the SSV
// adapter wraps the bytes produced here in a SignedSSVMessage. This package
// only handles the (un)marshaling of OBFT message bodies.
package wire

import (
	"errors"
	"fmt"

	"github.com/ssvlabs/ssv/protocol/v2/obft"
	base "github.com/ssvlabs/ssv/protocol/v2/obft/base"
	sharedwire "github.com/ssvlabs/ssv/protocol/v2/wire"
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

// Wire-level caps. Defined once in the parent obft package and re-exported
// here (and identically in twoab/wire) so both OBFT-family codecs share one
// reconciled set of bounds — see protocol/v2/obft/wire_caps.go. Layer indices
// are valid in [0, MaxLayers); counts are valid in [0, MaxLayers].
const (
	MaxLayers         = obft.MaxLayers
	MaxValueSize      = obft.MaxValueSize
	MaxSignatureSize  = obft.MaxSignatureSize
	MaxCiphertextSize = obft.MaxCiphertextSize
)

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
	out = sharedwire.AppendUint64(out, uint64(b.OperatorID))
	out = sharedwire.AppendUint64(out, uint64(b.Height))
	out = sharedwire.AppendUint32(out, uint32(b.Layer))      //nolint:gosec // bounds-checked above
	out = sharedwire.AppendUint32(out, uint32(len(b.Value))) //nolint:gosec // bounds-checked
	out = append(out, b.Value...)
	out = sharedwire.AppendUint32(out, uint32(len(b.LeaderSigma))) //nolint:gosec // bounds-checked
	out = append(out, b.LeaderSigma...)
	return out, nil
}

// DecodePhase1Bundle parses bytes produced by EncodePhase1Bundle.
func DecodePhase1Bundle(data []byte) (*base.Phase1Bundle, error) {
	r := sharedwire.NewReader(data)
	if err := r.ExpectVersion(Phase1BundleVersionV1, "phase-1 bundle"); err != nil {
		return nil, err
	}
	if err := r.ExpectBytes(ProtocolTag[:], "protocol tag"); err != nil {
		return nil, err
	}
	if err := r.ExpectByte(innerKindPhase1Bundle, "phase-1 bundle inner kind"); err != nil {
		return nil, err
	}
	var clusterID [32]byte
	if err := r.FixedBytes(clusterID[:], "cluster id"); err != nil {
		return nil, err
	}
	opID, err := r.Uint64("operator id")
	if err != nil {
		return nil, err
	}
	height, err := r.Uint64("height")
	if err != nil {
		return nil, err
	}
	layer, err := r.Uint32("layer")
	if err != nil {
		return nil, err
	}
	// Layer is uint32 on the wire but no real OBFT config has K > MaxLayers.
	// Reject early so a malformed message doesn't reach protocol validation.
	if layer >= MaxLayers {
		return nil, fmt.Errorf("wire: phase-1 bundle layer %d exceeds MaxLayers %d", layer, MaxLayers)
	}
	value, err := r.LengthPrefixed("phase-1 value", MaxValueSize)
	if err != nil {
		return nil, err
	}
	sig, err := r.LengthPrefixed("phase-1 LeaderSigma", MaxSignatureSize)
	if err != nil {
		return nil, err
	}
	if err := r.RequireEOF("phase-1 bundle"); err != nil {
		return nil, err
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
	out = sharedwire.AppendUint64(out, uint64(c.OperatorID))
	out = sharedwire.AppendUint64(out, uint64(c.Height))
	out = sharedwire.AppendUint16(out, uint16(len(c.Layers))) //nolint:gosec // MaxLayers <= uint16 max
	for i, el := range c.Layers {
		if len(el.Value) > MaxValueSize {
			return nil, fmt.Errorf("wire: commit layer %d value too long (%d)", i, len(el.Value))
		}
		if len(el.Ciphertext) > MaxCiphertextSize {
			return nil, fmt.Errorf("wire: commit layer %d ciphertext too long (%d)", i, len(el.Ciphertext))
		}
		out = sharedwire.AppendUint32(out, uint32(len(el.Value)))      //nolint:gosec // bounds-checked
		out = append(out, el.Value...)                                 //
		out = sharedwire.AppendUint32(out, uint32(len(el.Ciphertext))) //nolint:gosec // bounds-checked
		out = append(out, el.Ciphertext...)
	}
	out = sharedwire.AppendUint16(out, uint16(len(c.NRPartials))) //nolint:gosec // bounds-checked
	for _, p := range c.NRPartials {
		if p.Layer < 0 {
			return nil, fmt.Errorf("wire: commit NR partial has negative layer %d", p.Layer)
		}
		if len(p.PartialSig) > MaxSignatureSize {
			return nil, fmt.Errorf("wire: commit NR partial sig too long (%d)", len(p.PartialSig))
		}
		out = sharedwire.AppendUint32(out, uint32(p.Layer))           //nolint:gosec // bounds-checked
		out = sharedwire.AppendUint32(out, uint32(len(p.PartialSig))) //nolint:gosec // bounds-checked
		out = append(out, p.PartialSig...)
	}
	out = sharedwire.AppendUint16(out, uint16(len(c.Witnesses))) //nolint:gosec // bounds-checked
	for i, w := range c.Witnesses {
		if w.Layer < 0 {
			return nil, fmt.Errorf("wire: commit witness %d has negative layer %d", i, w.Layer)
		}
		if len(w.Sigma) > MaxSignatureSize {
			return nil, fmt.Errorf("wire: commit witness %d leader sigma too long (%d)", i, len(w.Sigma))
		}
		out = sharedwire.AppendUint32(out, uint32(w.Layer))      //nolint:gosec // bounds-checked
		out = sharedwire.AppendUint64(out, uint64(w.Leader))     //
		out = append(out, w.ValueRoot[:]...)                     // fixed 32 bytes
		out = sharedwire.AppendUint32(out, uint32(len(w.Sigma))) //nolint:gosec // bounds-checked
		out = append(out, w.Sigma...)
	}
	return out, nil
}

// DecodeCommit parses bytes produced by EncodeCommit.
func DecodeCommit(data []byte) (*base.Commit, error) {
	r := sharedwire.NewReader(data)
	if err := r.ExpectVersion(CommitVersionV1, "commit"); err != nil {
		return nil, err
	}
	if err := r.ExpectBytes(ProtocolTag[:], "protocol tag"); err != nil {
		return nil, err
	}
	if err := r.ExpectByte(innerKindCommit, "commit inner kind"); err != nil {
		return nil, err
	}
	var clusterID [32]byte
	if err := r.FixedBytes(clusterID[:], "cluster id"); err != nil {
		return nil, err
	}
	opID, err := r.Uint64("operator id")
	if err != nil {
		return nil, err
	}
	height, err := r.Uint64("height")
	if err != nil {
		return nil, err
	}
	numLayers, err := r.Uint16("layer count")
	if err != nil {
		return nil, err
	}
	if int(numLayers) > MaxLayers {
		return nil, fmt.Errorf("wire: commit declares %d σ layers (max %d)", numLayers, MaxLayers)
	}
	layers := make([]base.EncryptedLayer, numLayers)
	for i := uint16(0); i < numLayers; i++ {
		value, err := r.LengthPrefixed(fmt.Sprintf("layer %d value", i), MaxValueSize)
		if err != nil {
			return nil, err
		}
		ct, err := r.LengthPrefixed(fmt.Sprintf("layer %d ciphertext", i), MaxCiphertextSize)
		if err != nil {
			return nil, err
		}
		layers[i] = base.EncryptedLayer{
			Value:      base.Value(value),
			Ciphertext: ct,
		}
	}
	nrCount, err := r.Uint16("NR partial count")
	if err != nil {
		return nil, err
	}
	if int(nrCount) > MaxLayers {
		return nil, fmt.Errorf("wire: commit declares %d NR partials (max %d)", nrCount, MaxLayers)
	}
	partials := make([]base.NRPartial, nrCount)
	for i := uint16(0); i < nrCount; i++ {
		layer, err := r.Uint32(fmt.Sprintf("NR partial %d layer", i))
		if err != nil {
			return nil, err
		}
		if layer >= MaxLayers {
			return nil, fmt.Errorf("wire: NR partial %d layer %d exceeds MaxLayers %d", i, layer, MaxLayers)
		}
		sig, err := r.LengthPrefixed(fmt.Sprintf("NR partial %d sig", i), MaxSignatureSize)
		if err != nil {
			return nil, err
		}
		partials[i] = base.NRPartial{
			Layer:      int(layer),
			PartialSig: base.Signature(sig),
		}
	}
	witnessCount, err := r.Uint16("witness count")
	if err != nil {
		return nil, err
	}
	if int(witnessCount) > MaxWitnesses {
		return nil, fmt.Errorf("wire: commit declares %d witnesses (max %d)", witnessCount, MaxWitnesses)
	}
	witnesses := make([]base.LeaderSigmaWitness, witnessCount)
	for i := uint16(0); i < witnessCount; i++ {
		layer, err := r.Uint32(fmt.Sprintf("witness %d layer", i))
		if err != nil {
			return nil, err
		}
		if layer >= MaxLayers {
			return nil, fmt.Errorf("wire: witness %d layer %d exceeds MaxLayers %d", i, layer, MaxLayers)
		}
		leader, err := r.Uint64(fmt.Sprintf("witness %d leader", i))
		if err != nil {
			return nil, err
		}
		var valueRoot [32]byte
		if err := r.FixedBytes(valueRoot[:], fmt.Sprintf("witness %d valueRoot", i)); err != nil {
			return nil, err
		}
		sig, err := r.LengthPrefixed(fmt.Sprintf("witness %d leader sigma", i), MaxSignatureSize)
		if err != nil {
			return nil, err
		}
		witnesses[i] = base.LeaderSigmaWitness{
			Layer:     int(layer),
			Leader:    base.OperatorID(leader),
			ValueRoot: valueRoot,
			Sigma:     base.Signature(sig),
		}
	}
	if err := r.RequireEOF("commit"); err != nil {
		return nil, err
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
	out = sharedwire.AppendUint64(out, uint64(c.Height))
	out = sharedwire.AppendUint32(out, uint32(len(c.Value)))     //nolint:gosec // bounds-checked
	out = append(out, c.Value...)                                //
	out = sharedwire.AppendUint32(out, uint32(len(c.Signature))) //nolint:gosec // bounds-checked
	out = append(out, c.Signature...)
	return out, nil
}

// DecodeCertificate parses bytes produced by EncodeCertificate.
func DecodeCertificate(data []byte) (*base.Certificate, error) {
	r := sharedwire.NewReader(data)
	if err := r.ExpectVersion(CertificateVersionV1, "certificate"); err != nil {
		return nil, err
	}
	if err := r.ExpectBytes(ProtocolTag[:], "protocol tag"); err != nil {
		return nil, err
	}
	if err := r.ExpectByte(innerKindCertificate, "certificate inner kind"); err != nil {
		return nil, err
	}
	var clusterID [32]byte
	if err := r.FixedBytes(clusterID[:], "cluster id"); err != nil {
		return nil, err
	}
	height, err := r.Uint64("height")
	if err != nil {
		return nil, err
	}
	value, err := r.LengthPrefixed("certificate value", MaxValueSize)
	if err != nil {
		return nil, err
	}
	sig, err := r.LengthPrefixed("certificate signature", MaxSignatureSize)
	if err != nil {
		return nil, err
	}
	if err := r.RequireEOF("certificate"); err != nil {
		return nil, err
	}
	return &base.Certificate{
		ClusterID: clusterID,
		Height:    base.Height(height),
		Value:     base.Value(value),
		Signature: base.Signature(sig),
	}, nil
}
