// Package wire — encoders/decoders for 2abOBFT message bodies. See
// envelope.go for the wrapping/unwrapping layer.
package wire

import (
	"errors"
	"fmt"

	"github.com/ssvlabs/ssv/protocol/v2/obft"
	"github.com/ssvlabs/ssv/protocol/v2/obft/twoab"
	sharedwire "github.com/ssvlabs/ssv/protocol/v2/wire"
)

// Wire format versions per message kind. Bumped when the on-the-wire
// layout changes incompatibly. Encoders write the current version;
// decoders accept only versions they understand.
//
// Cluster-cutover policy: operators MUST be rolled in lockstep across
// any wire-version bump. Mixed-version clusters will see all cross-
// version messages fail at the version-byte check inside the relevant
// Decode* function, manifesting as silent quorum starvation (the
// upgraded ops reject the legacy ops' bundles/ValueMsgs as malformed,
// and vice versa). There is no cross-version compatibility layer; the
// decoder accepts exactly one version per kind. Pre-deployment
// verification SHOULD ensure all cluster members run the same protocol
// minor version before a version-bumping release is rolled out.
const (
	// Phase1BundleVersionV3 carries the leader witness field LeaderSigma at
	// EVERY layer, not just L_0 (see docs/2abOBFT.md §Phase 1): the
	// witness is required and processed at all layers.
	Phase1BundleVersionV3 byte = 0x03
	// ValueMsgVersionV4 carries a Witnesses []LayerWitness section (see
	// docs/2abOBFT.md §Phase 2a, Forwarded leader witnesses), so a
	// KindValue forwards the leader σ-witness at every layer the emitter
	// is σ-side on (L_0 always, plus deeper fall-through layers), not just
	// L_0. It also carries L0Partial, the emitter's own σ partial signed
	// at emit time.
	ValueMsgVersionV4   byte = 0x04
	NoValueMsgVersionV1 byte = 0x01
	// CommitVersionV2: the σ-side lives in KindValue.L0Partial, so Commit
	// carries no L0Value field.
	CommitVersionV2      byte = 0x02
	CertificateVersionV1 byte = 0x01
)

// ProtocolTag is the fixed 16-byte literal stamped into every inner
// 2abOBFT message's signed bytes. Decoders reject messages with a
// mismatching tag.
//
// Per spec §Phase 1 / §Phase 2a / §Phase 2b auth envelope: protocol_tag
// = "2abOBFT". Padded to 16 bytes with NULs to fit a fixed-size field
// (the spec string is 7 chars; bare OBFT uses an 8-byte tag, but 2ab's
// name doesn't fit 8 bytes, so we use 16 — also more headroom for future
// protocol identifiers).
//
// This is the load-bearing domain separation against bare-OBFT envelopes
// — a base-encoded message decoded with twoab/wire.Unwrap fails at the
// ProtocolTag check.
var ProtocolTag = [16]byte{
	'2', 'a', 'b', 'O', 'B', 'F', 'T',
	0, 0, 0, 0, 0, 0, 0, 0, 0,
}

// Inner-kind tag: each message type stamps its own one-byte kind into the
// inner signed bytes (defense-in-depth on top of the outer envelope's kind
// byte). Mismatch with the decoder's expected kind is a structural error.
const (
	innerKindPhase1Bundle byte = 0x01
	innerKindValueMsg     byte = 0x02
	innerKindNoValueMsg   byte = 0x03
	innerKindCommit       byte = 0x04
	innerKindCertificate  byte = 0x05
)

// Wire-level caps. Defined once in the parent obft package and re-exported
// here (and identically in base/wire) so both OBFT-family codecs share one
// reconciled set of bounds — see protocol/v2/obft/wire_caps.go. Layer indices
// are valid in [0, MaxLayers); counts are valid in [0, MaxLayers].
const (
	MaxLayers         = obft.MaxLayers
	MaxValueSize      = obft.MaxValueSize
	MaxSignatureSize  = obft.MaxSignatureSize
	MaxCiphertextSize = obft.MaxCiphertextSize
)

// ---------- Phase1Bundle ----------

// EncodePhase1Bundle serializes a Phase-1 bundle.
//
// Format (version 0x03 — per-layer LeaderSigma):
//
//	[1]   version
//	[16]  ProtocolTag    "2abOBFT" + 9 NULs
//	[1]   inner kind     = innerKindPhase1Bundle
//	[32]  ClusterID
//	[8]   OperatorID     (uint64 big-endian)
//	[8]   Height         (uint64 big-endian)
//	[4]   Layer          (uint32 big-endian)
//	[4]   Value length   (uint32 big-endian)
//	[Value bytes]
//	[4]   LeaderSigma length  (uint32 big-endian)
//	[LeaderSigma bytes]
func EncodePhase1Bundle(b *twoab.Phase1Bundle) ([]byte, error) {
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

	size := 1 + 16 + 1 + 32 + 8 + 8 + 4 + 4 + len(b.Value) + 4 + len(b.LeaderSigma)
	out := make([]byte, 0, size)
	out = append(out, Phase1BundleVersionV3)
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
func DecodePhase1Bundle(data []byte) (*twoab.Phase1Bundle, error) {
	r := sharedwire.NewReader(data)
	if err := r.ExpectVersion(Phase1BundleVersionV3, "phase-1 bundle"); err != nil {
		return nil, err
	}
	if err := r.ExpectBytes(ProtocolTag[:], "protocol tag"); err != nil {
		return nil, err
	}
	if err := r.ExpectByte(innerKindPhase1Bundle, "phase-1 bundle inner kind"); err != nil {
		return nil, err
	}
	var clusterID [32]byte
	if err := r.FixedBytes(clusterID[:], "phase-1 bundle cluster_id"); err != nil {
		return nil, err
	}
	opID, err := r.Uint64("phase-1 bundle operator_id")
	if err != nil {
		return nil, err
	}
	height, err := r.Uint64("phase-1 bundle height")
	if err != nil {
		return nil, err
	}
	layer, err := r.Uint32("phase-1 bundle layer")
	if err != nil {
		return nil, err
	}
	if layer >= MaxLayers {
		return nil, fmt.Errorf("wire: phase-1 bundle layer %d exceeds MaxLayers %d", layer, MaxLayers)
	}
	value, err := r.LengthPrefixed("phase-1 bundle value", MaxValueSize)
	if err != nil {
		return nil, err
	}
	witness, err := r.LengthPrefixed("phase-1 bundle LeaderSigma", MaxSignatureSize)
	if err != nil {
		return nil, err
	}
	if err := r.RequireEOF("phase-1 bundle"); err != nil {
		return nil, err
	}
	return &twoab.Phase1Bundle{
		ClusterID:   clusterID,
		OperatorID:  twoab.OperatorID(opID),
		Height:      twoab.Height(height),
		Layer:       int(layer), //nolint:gosec // bounds-checked above
		Value:       twoab.Value(value),
		LeaderSigma: twoab.Signature(witness),
	}, nil
}

// ---------- ValueMsg ----------

// EncodeValueMsg serializes a Phase-2a ValueMsg envelope.
//
// Format (version 0x04 — Witnesses[] section):
//
//	[1]  version
//	[16] ProtocolTag
//	[1]  inner kind     = innerKindValueMsg
//	[32] ClusterID
//	[8]  OperatorID
//	[8]  Height
//	[4]  V length
//	[V bytes]
//	[32] ValueRoot
//	[Witnesses block — see encodeLayerWitnesses]
//	[4]  L0Partial length
//	[L0Partial bytes]
//	[LayerEntries block — see encodeLayerEntries]
func EncodeValueMsg(v *twoab.ValueMsg) ([]byte, error) {
	if v == nil {
		return nil, errors.New("wire: nil ValueMsg")
	}
	if len(v.V) > MaxValueSize {
		return nil, fmt.Errorf("wire: ValueMsg V too long (%d)", len(v.V))
	}
	if err := preflightLayerWitnesses(v.Witnesses, "ValueMsg"); err != nil {
		return nil, err
	}
	if len(v.L0Partial) > MaxSignatureSize {
		return nil, fmt.Errorf("wire: ValueMsg L0Partial too long (%d)", len(v.L0Partial))
	}
	if err := preflightLayerEntries(v.LayerEntries, "ValueMsg"); err != nil {
		return nil, err
	}

	out := make([]byte, 0, 1+16+1+32+8+8+4+len(v.V)+32+4+4+len(v.L0Partial)+4)
	out = append(out, ValueMsgVersionV4)
	out = append(out, ProtocolTag[:]...)
	out = append(out, innerKindValueMsg)
	out = append(out, v.ClusterID[:]...)
	out = sharedwire.AppendUint64(out, uint64(v.OperatorID))
	out = sharedwire.AppendUint64(out, uint64(v.Height))
	out = sharedwire.AppendUint32(out, uint32(len(v.V))) //nolint:gosec // bounds-checked
	out = append(out, v.V...)
	out = append(out, v.ValueRoot[:]...)
	out = encodeLayerWitnesses(out, v.Witnesses)
	out = sharedwire.AppendUint32(out, uint32(len(v.L0Partial))) //nolint:gosec // bounds-checked
	out = append(out, v.L0Partial...)
	out = encodeLayerEntries(out, v.LayerEntries)
	return out, nil
}

// DecodeValueMsg parses bytes produced by EncodeValueMsg.
func DecodeValueMsg(data []byte) (*twoab.ValueMsg, error) {
	r := sharedwire.NewReader(data)
	if err := r.ExpectVersion(ValueMsgVersionV4, "ValueMsg"); err != nil {
		return nil, err
	}
	if err := r.ExpectBytes(ProtocolTag[:], "protocol tag"); err != nil {
		return nil, err
	}
	if err := r.ExpectByte(innerKindValueMsg, "ValueMsg inner kind"); err != nil {
		return nil, err
	}
	var clusterID [32]byte
	if err := r.FixedBytes(clusterID[:], "ValueMsg cluster_id"); err != nil {
		return nil, err
	}
	opID, err := r.Uint64("ValueMsg operator_id")
	if err != nil {
		return nil, err
	}
	height, err := r.Uint64("ValueMsg height")
	if err != nil {
		return nil, err
	}
	v, err := r.LengthPrefixed("ValueMsg V", MaxValueSize)
	if err != nil {
		return nil, err
	}
	var valueRoot [32]byte
	if err := r.FixedBytes(valueRoot[:], "ValueMsg value_root"); err != nil {
		return nil, err
	}
	witnesses, err := decodeLayerWitnesses(r, "ValueMsg")
	if err != nil {
		return nil, err
	}
	partial, err := r.LengthPrefixed("ValueMsg L0Partial", MaxSignatureSize)
	if err != nil {
		return nil, err
	}
	entries, err := decodeLayerEntries(r, "ValueMsg")
	if err != nil {
		return nil, err
	}
	if err := r.RequireEOF("ValueMsg"); err != nil {
		return nil, err
	}
	return &twoab.ValueMsg{
		ClusterID:    clusterID,
		OperatorID:   twoab.OperatorID(opID),
		Height:       twoab.Height(height),
		V:            twoab.Value(v),
		ValueRoot:    valueRoot,
		Witnesses:    witnesses,
		L0Partial:    twoab.Signature(partial),
		LayerEntries: entries,
	}, nil
}

// ---------- NoValueMsg ----------

// EncodeNoValueMsg serializes a Phase-2a NoValueMsg envelope.
//
// Format (version 0x01):
//
//	[1]  version
//	[16] ProtocolTag
//	[1]  inner kind     = innerKindNoValueMsg
//	[32] ClusterID
//	[8]  OperatorID
//	[8]  Height
//	[LayerEntries block]
func EncodeNoValueMsg(nv *twoab.NoValueMsg) ([]byte, error) {
	if nv == nil {
		return nil, errors.New("wire: nil NoValueMsg")
	}
	if err := preflightLayerEntries(nv.LayerEntries, "NoValueMsg"); err != nil {
		return nil, err
	}

	out := make([]byte, 0, 1+16+1+32+8+8+4)
	out = append(out, NoValueMsgVersionV1)
	out = append(out, ProtocolTag[:]...)
	out = append(out, innerKindNoValueMsg)
	out = append(out, nv.ClusterID[:]...)
	out = sharedwire.AppendUint64(out, uint64(nv.OperatorID))
	out = sharedwire.AppendUint64(out, uint64(nv.Height))
	out = encodeLayerEntries(out, nv.LayerEntries)
	return out, nil
}

// DecodeNoValueMsg parses bytes produced by EncodeNoValueMsg.
func DecodeNoValueMsg(data []byte) (*twoab.NoValueMsg, error) {
	r := sharedwire.NewReader(data)
	if err := r.ExpectVersion(NoValueMsgVersionV1, "NoValueMsg"); err != nil {
		return nil, err
	}
	if err := r.ExpectBytes(ProtocolTag[:], "protocol tag"); err != nil {
		return nil, err
	}
	if err := r.ExpectByte(innerKindNoValueMsg, "NoValueMsg inner kind"); err != nil {
		return nil, err
	}
	var clusterID [32]byte
	if err := r.FixedBytes(clusterID[:], "NoValueMsg cluster_id"); err != nil {
		return nil, err
	}
	opID, err := r.Uint64("NoValueMsg operator_id")
	if err != nil {
		return nil, err
	}
	height, err := r.Uint64("NoValueMsg height")
	if err != nil {
		return nil, err
	}
	entries, err := decodeLayerEntries(r, "NoValueMsg")
	if err != nil {
		return nil, err
	}
	if err := r.RequireEOF("NoValueMsg"); err != nil {
		return nil, err
	}
	return &twoab.NoValueMsg{
		ClusterID:    clusterID,
		OperatorID:   twoab.OperatorID(opID),
		Height:       twoab.Height(height),
		LayerEntries: entries,
	}, nil
}

// ---------- Commit ----------

// EncodeCommit serializes a Phase-2b (or Phase-2a NRDirect) Commit envelope.
//
// Format (version 0x02 — no L0Value field):
//
//	[1]  version
//	[16] ProtocolTag
//	[1]  inner kind     = innerKindCommit
//	[32] ClusterID
//	[8]  OperatorID
//	[8]  Height
//	[1]  Side
//	[4]  L0Partial length
//	[L0Partial bytes]
//	[LayerEntries block — empty (count=0) for Side != NRDirect]
func EncodeCommit(c *twoab.Commit) ([]byte, error) {
	if c == nil {
		return nil, errors.New("wire: nil Commit")
	}
	if c.Side == twoab.CommitSideUnspecified {
		return nil, errors.New("wire: Commit Side is unspecified")
	}
	if len(c.L0Partial) > MaxSignatureSize {
		return nil, fmt.Errorf("wire: Commit L0Partial too long (%d)", len(c.L0Partial))
	}
	if err := preflightLayerEntries(c.LayerEntries, "Commit"); err != nil {
		return nil, err
	}

	out := make([]byte, 0, 1+16+1+32+8+8+1+4+len(c.L0Partial)+4)
	out = append(out, CommitVersionV2)
	out = append(out, ProtocolTag[:]...)
	out = append(out, innerKindCommit)
	out = append(out, c.ClusterID[:]...)
	out = sharedwire.AppendUint64(out, uint64(c.OperatorID))
	out = sharedwire.AppendUint64(out, uint64(c.Height))
	out = append(out, byte(c.Side))
	out = sharedwire.AppendUint32(out, uint32(len(c.L0Partial))) //nolint:gosec // bounds-checked
	out = append(out, c.L0Partial...)
	out = encodeLayerEntries(out, c.LayerEntries)
	return out, nil
}

// DecodeCommit parses bytes produced by EncodeCommit.
func DecodeCommit(data []byte) (*twoab.Commit, error) {
	r := sharedwire.NewReader(data)
	if err := r.ExpectVersion(CommitVersionV2, "Commit"); err != nil {
		return nil, err
	}
	if err := r.ExpectBytes(ProtocolTag[:], "protocol tag"); err != nil {
		return nil, err
	}
	if err := r.ExpectByte(innerKindCommit, "Commit inner kind"); err != nil {
		return nil, err
	}
	var clusterID [32]byte
	if err := r.FixedBytes(clusterID[:], "Commit cluster_id"); err != nil {
		return nil, err
	}
	opID, err := r.Uint64("Commit operator_id")
	if err != nil {
		return nil, err
	}
	height, err := r.Uint64("Commit height")
	if err != nil {
		return nil, err
	}
	sideByte, err := r.Byte("Commit side")
	if err != nil {
		return nil, err
	}
	side := twoab.CommitSide(sideByte)
	switch side {
	case twoab.CommitSideNR, twoab.CommitSideNRDirect:
		// valid
	default:
		// 0x01 is not a valid Commit side; deliberately rejected here to
		// make wire-version drift visible. ValidateCommit at the Instance
		// layer also rejects it.
		return nil, fmt.Errorf("wire: Commit side 0x%02x is invalid (only NR=0x02 and NRDirect=0x03 are valid)", sideByte)
	}
	l0Partial, err := r.LengthPrefixed("Commit L0Partial", MaxSignatureSize)
	if err != nil {
		return nil, err
	}
	entries, err := decodeLayerEntries(r, "Commit")
	if err != nil {
		return nil, err
	}
	if err := r.RequireEOF("Commit"); err != nil {
		return nil, err
	}
	return &twoab.Commit{
		ClusterID:    clusterID,
		OperatorID:   twoab.OperatorID(opID),
		Height:       twoab.Height(height),
		Side:         side,
		L0Partial:    twoab.Signature(l0Partial),
		LayerEntries: entries,
	}, nil
}

// ---------- Certificate ----------

// EncodeCertificate serializes a final-certificate.
//
// Format (version 0x01):
//
//	[1]  version
//	[16] ProtocolTag
//	[1]  inner kind     = innerKindCertificate
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
	if len(c.Value) > MaxValueSize {
		return nil, fmt.Errorf("wire: certificate value too long (%d)", len(c.Value))
	}
	if len(c.Signature) > MaxSignatureSize {
		return nil, fmt.Errorf("wire: certificate signature too long (%d)", len(c.Signature))
	}

	size := 1 + 16 + 1 + 32 + 8 + 4 + len(c.Value) + 4 + len(c.Signature)
	out := make([]byte, 0, size)
	out = append(out, CertificateVersionV1)
	out = append(out, ProtocolTag[:]...)
	out = append(out, innerKindCertificate)
	out = append(out, c.ClusterID[:]...)
	out = sharedwire.AppendUint64(out, uint64(c.Height))
	out = sharedwire.AppendUint32(out, uint32(len(c.Value))) //nolint:gosec // bounds-checked
	out = append(out, c.Value...)
	out = sharedwire.AppendUint32(out, uint32(len(c.Signature))) //nolint:gosec // bounds-checked
	out = append(out, c.Signature...)
	return out, nil
}

// DecodeCertificate parses bytes produced by EncodeCertificate.
func DecodeCertificate(data []byte) (*twoab.Certificate, error) {
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
	if err := r.FixedBytes(clusterID[:], "certificate cluster_id"); err != nil {
		return nil, err
	}
	height, err := r.Uint64("certificate height")
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
	return &twoab.Certificate{
		ClusterID: clusterID,
		Height:    twoab.Height(height),
		Value:     twoab.Value(value),
		Signature: twoab.Signature(sig),
	}, nil
}

// ---------- LayerEntries block ----------

// LayerEntries block encoding (shared by ValueMsg, NoValueMsg, and Commit
// when Side=NRDirect):
//
//	[4]  Count (uint32)
//	for each entry:
//	    [4] Layer (uint32)
//	    [1] Kind
//	    [4] V length
//	    [V bytes]
//	    [4] Payload length
//	    [Payload bytes]

func preflightLayerEntries(entries []twoab.LayerEntry, kindLabel string) error {
	if len(entries) > MaxLayers {
		return fmt.Errorf("wire: %s has %d LayerEntries, max %d", kindLabel, len(entries), MaxLayers)
	}
	for i, e := range entries {
		if e.Layer < 0 {
			return fmt.Errorf("wire: %s LayerEntries[%d] has negative Layer %d", kindLabel, i, e.Layer)
		}
		if len(e.V) > MaxValueSize {
			return fmt.Errorf("wire: %s LayerEntries[%d] V too long (%d)", kindLabel, i, len(e.V))
		}
		if len(e.Payload) > MaxCiphertextSize {
			return fmt.Errorf("wire: %s LayerEntries[%d] Payload too long (%d)", kindLabel, i, len(e.Payload))
		}
	}
	return nil
}

func encodeLayerEntries(out []byte, entries []twoab.LayerEntry) []byte {
	out = sharedwire.AppendUint32(out, uint32(len(entries))) //nolint:gosec // bounds-checked by preflight
	for _, e := range entries {
		out = sharedwire.AppendUint32(out, uint32(e.Layer)) //nolint:gosec // bounds-checked by preflight
		out = append(out, byte(e.Kind))
		out = sharedwire.AppendUint32(out, uint32(len(e.V))) //nolint:gosec // bounds-checked by preflight
		out = append(out, e.V...)
		out = sharedwire.AppendUint32(out, uint32(len(e.Payload))) //nolint:gosec // bounds-checked by preflight
		out = append(out, e.Payload...)
	}
	return out
}

func decodeLayerEntries(r *sharedwire.Reader, kindLabel string) ([]twoab.LayerEntry, error) {
	count, err := r.Uint32(fmt.Sprintf("%s LayerEntries count", kindLabel))
	if err != nil {
		return nil, err
	}
	if count > MaxLayers {
		return nil, fmt.Errorf("wire: %s LayerEntries count %d exceeds MaxLayers %d",
			kindLabel, count, MaxLayers)
	}
	entries := make([]twoab.LayerEntry, count)
	for i := uint32(0); i < count; i++ {
		layer, err := r.Uint32(fmt.Sprintf("%s LayerEntries[%d] layer", kindLabel, i))
		if err != nil {
			return nil, err
		}
		if layer >= MaxLayers {
			return nil, fmt.Errorf("wire: %s LayerEntries[%d] layer %d exceeds MaxLayers %d",
				kindLabel, i, layer, MaxLayers)
		}
		kindByte, err := r.Byte(fmt.Sprintf("%s LayerEntries[%d] kind", kindLabel, i))
		if err != nil {
			return nil, err
		}
		kind := twoab.LayerEntryKind(kindByte)
		switch kind {
		case twoab.LayerEntryEmpty, twoab.LayerEntrySigmaChained, twoab.LayerEntryNRPlaintext:
			// valid
		default:
			return nil, fmt.Errorf("wire: %s LayerEntries[%d] kind 0x%02x is invalid",
				kindLabel, i, kindByte)
		}
		v, err := r.LengthPrefixed(fmt.Sprintf("%s LayerEntries[%d] V", kindLabel, i), MaxValueSize)
		if err != nil {
			return nil, err
		}
		payload, err := r.LengthPrefixed(fmt.Sprintf("%s LayerEntries[%d] Payload", kindLabel, i), MaxCiphertextSize)
		if err != nil {
			return nil, err
		}
		entries[i] = twoab.LayerEntry{
			Layer:   int(layer), //nolint:gosec // bounds-checked above
			Kind:    kind,
			V:       twoab.Value(v),
			Payload: payload,
		}
	}
	return entries, nil
}

// ---------- LayerWitnesses ----------

func preflightLayerWitnesses(ws []twoab.LayerWitness, kindLabel string) error {
	if len(ws) > MaxLayers {
		return fmt.Errorf("wire: %s has %d Witnesses, max %d", kindLabel, len(ws), MaxLayers)
	}
	for i, w := range ws {
		if w.Layer < 0 {
			return fmt.Errorf("wire: %s Witnesses[%d] has negative Layer %d", kindLabel, i, w.Layer)
		}
		if len(w.Witness) > MaxSignatureSize {
			return fmt.Errorf("wire: %s Witnesses[%d] Witness too long (%d)", kindLabel, i, len(w.Witness))
		}
	}
	return nil
}

func encodeLayerWitnesses(out []byte, ws []twoab.LayerWitness) []byte {
	out = sharedwire.AppendUint32(out, uint32(len(ws))) //nolint:gosec // bounds-checked by preflight
	for _, w := range ws {
		out = sharedwire.AppendUint32(out, uint32(w.Layer)) //nolint:gosec // bounds-checked by preflight
		out = append(out, w.ValueRoot[:]...)
		out = sharedwire.AppendUint32(out, uint32(len(w.Witness))) //nolint:gosec // bounds-checked by preflight
		out = append(out, w.Witness...)
	}
	return out
}

func decodeLayerWitnesses(r *sharedwire.Reader, kindLabel string) ([]twoab.LayerWitness, error) {
	count, err := r.Uint32(fmt.Sprintf("%s Witnesses count", kindLabel))
	if err != nil {
		return nil, err
	}
	if count > MaxLayers {
		return nil, fmt.Errorf("wire: %s Witnesses count %d exceeds MaxLayers %d",
			kindLabel, count, MaxLayers)
	}
	ws := make([]twoab.LayerWitness, count)
	for i := uint32(0); i < count; i++ {
		layer, err := r.Uint32(fmt.Sprintf("%s Witnesses[%d] layer", kindLabel, i))
		if err != nil {
			return nil, err
		}
		if layer >= MaxLayers {
			return nil, fmt.Errorf("wire: %s Witnesses[%d] layer %d exceeds MaxLayers %d",
				kindLabel, i, layer, MaxLayers)
		}
		var root [32]byte
		if err := r.FixedBytes(root[:], fmt.Sprintf("%s Witnesses[%d] value_root", kindLabel, i)); err != nil {
			return nil, err
		}
		wit, err := r.LengthPrefixed(fmt.Sprintf("%s Witnesses[%d] Witness", kindLabel, i), MaxSignatureSize)
		if err != nil {
			return nil, err
		}
		ws[i] = twoab.LayerWitness{
			Layer:     int(layer), //nolint:gosec // bounds-checked above
			ValueRoot: root,
			Witness:   twoab.Signature(wit),
		}
	}
	return ws, nil
}
