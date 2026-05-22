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
	// Phase1BundleVersionV3 renames the leader witness field L0Witness →
	// LWitness and carries it at EVERY layer (Op8 / Phase D), not just L_0
	// (see docs/2abOBFT-REDESIGN-PLAN.md §Phase D / Op8). The byte layout is
	// unchanged from V2 (which added the L_0-only L0Witness per Op3); the
	// bump signals the semantic change (witness now required + processed at
	// all layers). Wire-incompatible with V2/V1.
	Phase1BundleVersionV3 byte = 0x03
	// ValueMsgVersionV3 adds the emitter's own σ partial on V at L_0
	// (L0Partial) per Op5 of the healthy-path optimization (see
	// docs/2abOBFT-REDESIGN-PLAN.md §Healthy-path optimizations / Op5).
	// KindValue under V3 is the terminal σ-side emission at L_0 — the
	// pre-Op5 KindCommit-Signed wire kind no longer exists. Wire-
	// incompatible with V2 (which had no L0Partial) and V1 (no L0Witness
	// or L0Partial). Bundles both Op11's L0Witness (forwarded leader
	// witness, byte-for-byte from the Phase-1 bundle) and Op5's
	// L0Partial (the emitter's own σ partial signed at KindValue emit
	// time).
	ValueMsgVersionV3    byte = 0x03
	NoValueMsgVersionV1  byte = 0x01
	CommitVersionV1      byte = 0x01
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

// MaxLayers caps the number of layers a Commit / Phase-2a emission can
// declare on the wire. Real 2abOBFT configs use K ≤ n ≤ 13 in SSV;
// anything past 32 is almost certainly malformed/malicious.
const MaxLayers = 32

// MaxFieldSize caps individual length-prefixed fields (values, ciphertexts,
// signatures). 16 MiB is conservative — realistic 2abOBFT messages are far
// smaller. The 16 MiB cap matches base/wire's choice so the same shared
// SignedSSVMessage envelope sizing applies uniformly across protocols.
const MaxFieldSize = 16 * 1024 * 1024

// ---------- Phase1Bundle ----------

// EncodePhase1Bundle serializes a Phase-1 bundle.
//
// Format (version 0x03 — per-layer LWitness post Op8; byte layout identical
// to V2's L_0-only L0Witness):
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
//	[4]   LWitness length  (uint32 big-endian)
//	[LWitness bytes]
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
	if len(b.LWitness) > MaxFieldSize {
		return nil, fmt.Errorf("wire: phase-1 bundle LWitness too long (%d)", len(b.LWitness))
	}

	size := 1 + 16 + 1 + 32 + 8 + 8 + 4 + 4 + len(b.Value) + 4 + len(b.LWitness)
	out := make([]byte, 0, size)
	out = append(out, Phase1BundleVersionV3)
	out = append(out, ProtocolTag[:]...)
	out = append(out, innerKindPhase1Bundle)
	out = append(out, b.ClusterID[:]...)
	out = appendUint64(out, uint64(b.OperatorID))
	out = appendUint64(out, uint64(b.Height))
	out = appendUint32(out, uint32(b.Layer))      //nolint:gosec // bounds-checked above
	out = appendUint32(out, uint32(len(b.Value))) //nolint:gosec // bounds-checked
	out = append(out, b.Value...)
	out = appendUint32(out, uint32(len(b.LWitness))) //nolint:gosec // bounds-checked
	out = append(out, b.LWitness...)
	return out, nil
}

// DecodePhase1Bundle parses bytes produced by EncodePhase1Bundle.
func DecodePhase1Bundle(data []byte) (*twoab.Phase1Bundle, error) {
	r := newReader(data)
	if err := readVersion(r, Phase1BundleVersionV3, "phase-1 bundle"); err != nil {
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
	witness, err := r.readLengthPrefixed("phase-1 bundle LWitness")
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
		LWitness:   twoab.Signature(witness),
	}, nil
}

// ---------- ValueMsg ----------

// EncodeValueMsg serializes a Phase-2a ValueMsg envelope.
//
// Format (version 0x03 — adds L0Partial post Op5):
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
//	[4]  L0Witness length
//	[L0Witness bytes]
//	[4]  L0Partial length
//	[L0Partial bytes]
//	[LayerEntries block — see encodeLayerEntries]
func EncodeValueMsg(v *twoab.ValueMsg) ([]byte, error) {
	if v == nil {
		return nil, errors.New("wire: nil ValueMsg")
	}
	if len(v.V) > MaxFieldSize {
		return nil, fmt.Errorf("wire: ValueMsg V too long (%d)", len(v.V))
	}
	if len(v.L0Witness) > MaxFieldSize {
		return nil, fmt.Errorf("wire: ValueMsg L0Witness too long (%d)", len(v.L0Witness))
	}
	if len(v.L0Partial) > MaxFieldSize {
		return nil, fmt.Errorf("wire: ValueMsg L0Partial too long (%d)", len(v.L0Partial))
	}
	if err := preflightLayerEntries(v.LayerEntries, "ValueMsg"); err != nil {
		return nil, err
	}

	out := make([]byte, 0, 1+16+1+32+8+8+4+len(v.V)+32+4+len(v.L0Witness)+4+len(v.L0Partial)+4)
	out = append(out, ValueMsgVersionV3)
	out = append(out, ProtocolTag[:]...)
	out = append(out, innerKindValueMsg)
	out = append(out, v.ClusterID[:]...)
	out = appendUint64(out, uint64(v.OperatorID))
	out = appendUint64(out, uint64(v.Height))
	out = appendUint32(out, uint32(len(v.V))) //nolint:gosec // bounds-checked
	out = append(out, v.V...)
	out = append(out, v.ValueRoot[:]...)
	out = appendUint32(out, uint32(len(v.L0Witness))) //nolint:gosec // bounds-checked
	out = append(out, v.L0Witness...)
	out = appendUint32(out, uint32(len(v.L0Partial))) //nolint:gosec // bounds-checked
	out = append(out, v.L0Partial...)
	out = encodeLayerEntries(out, v.LayerEntries)
	return out, nil
}

// DecodeValueMsg parses bytes produced by EncodeValueMsg.
func DecodeValueMsg(data []byte) (*twoab.ValueMsg, error) {
	r := newReader(data)
	if err := readVersion(r, ValueMsgVersionV3, "ValueMsg"); err != nil {
		return nil, err
	}
	if err := readProtocolTag(r); err != nil {
		return nil, err
	}
	if err := readInnerKind(r, innerKindValueMsg, "ValueMsg"); err != nil {
		return nil, err
	}
	var clusterID [32]byte
	if err := r.readBytes(clusterID[:]); err != nil {
		return nil, fmt.Errorf("wire: ValueMsg cluster_id: %w", err)
	}
	opID, err := r.readUint64()
	if err != nil {
		return nil, fmt.Errorf("wire: ValueMsg operator_id: %w", err)
	}
	height, err := r.readUint64()
	if err != nil {
		return nil, fmt.Errorf("wire: ValueMsg height: %w", err)
	}
	v, err := r.readLengthPrefixed("ValueMsg V")
	if err != nil {
		return nil, err
	}
	var valueRoot [32]byte
	if err := r.readBytes(valueRoot[:]); err != nil {
		return nil, fmt.Errorf("wire: ValueMsg value_root: %w", err)
	}
	witness, err := r.readLengthPrefixed("ValueMsg L0Witness")
	if err != nil {
		return nil, err
	}
	partial, err := r.readLengthPrefixed("ValueMsg L0Partial")
	if err != nil {
		return nil, err
	}
	entries, err := decodeLayerEntries(r, "ValueMsg")
	if err != nil {
		return nil, err
	}
	if err := r.requireEOF("ValueMsg"); err != nil {
		return nil, err
	}
	return &twoab.ValueMsg{
		ClusterID:    clusterID,
		OperatorID:   twoab.OperatorID(opID),
		Height:       twoab.Height(height),
		V:            twoab.Value(v),
		ValueRoot:    valueRoot,
		L0Witness:    twoab.Signature(witness),
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
	out = appendUint64(out, uint64(nv.OperatorID))
	out = appendUint64(out, uint64(nv.Height))
	out = encodeLayerEntries(out, nv.LayerEntries)
	return out, nil
}

// DecodeNoValueMsg parses bytes produced by EncodeNoValueMsg.
func DecodeNoValueMsg(data []byte) (*twoab.NoValueMsg, error) {
	r := newReader(data)
	if err := readVersion(r, NoValueMsgVersionV1, "NoValueMsg"); err != nil {
		return nil, err
	}
	if err := readProtocolTag(r); err != nil {
		return nil, err
	}
	if err := readInnerKind(r, innerKindNoValueMsg, "NoValueMsg"); err != nil {
		return nil, err
	}
	var clusterID [32]byte
	if err := r.readBytes(clusterID[:]); err != nil {
		return nil, fmt.Errorf("wire: NoValueMsg cluster_id: %w", err)
	}
	opID, err := r.readUint64()
	if err != nil {
		return nil, fmt.Errorf("wire: NoValueMsg operator_id: %w", err)
	}
	height, err := r.readUint64()
	if err != nil {
		return nil, fmt.Errorf("wire: NoValueMsg height: %w", err)
	}
	entries, err := decodeLayerEntries(r, "NoValueMsg")
	if err != nil {
		return nil, err
	}
	if err := r.requireEOF("NoValueMsg"); err != nil {
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
// Format (version 0x01):
//
//	[1]  version
//	[16] ProtocolTag
//	[1]  inner kind     = innerKindCommit
//	[32] ClusterID
//	[8]  OperatorID
//	[8]  Height
//	[1]  Side
//	[4]  L0Value length
//	[L0Value bytes]
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
	if len(c.L0Value) > MaxFieldSize {
		return nil, fmt.Errorf("wire: Commit L0Value too long (%d)", len(c.L0Value))
	}
	if len(c.L0Partial) > MaxFieldSize {
		return nil, fmt.Errorf("wire: Commit L0Partial too long (%d)", len(c.L0Partial))
	}
	if err := preflightLayerEntries(c.LayerEntries, "Commit"); err != nil {
		return nil, err
	}

	out := make([]byte, 0, 1+16+1+32+8+8+1+4+len(c.L0Value)+4+len(c.L0Partial)+4)
	out = append(out, CommitVersionV1)
	out = append(out, ProtocolTag[:]...)
	out = append(out, innerKindCommit)
	out = append(out, c.ClusterID[:]...)
	out = appendUint64(out, uint64(c.OperatorID))
	out = appendUint64(out, uint64(c.Height))
	out = append(out, byte(c.Side))
	out = appendUint32(out, uint32(len(c.L0Value))) //nolint:gosec // bounds-checked
	out = append(out, c.L0Value...)
	out = appendUint32(out, uint32(len(c.L0Partial))) //nolint:gosec // bounds-checked
	out = append(out, c.L0Partial...)
	out = encodeLayerEntries(out, c.LayerEntries)
	return out, nil
}

// DecodeCommit parses bytes produced by EncodeCommit.
func DecodeCommit(data []byte) (*twoab.Commit, error) {
	r := newReader(data)
	if err := readVersion(r, CommitVersionV1, "Commit"); err != nil {
		return nil, err
	}
	if err := readProtocolTag(r); err != nil {
		return nil, err
	}
	if err := readInnerKind(r, innerKindCommit, "Commit"); err != nil {
		return nil, err
	}
	var clusterID [32]byte
	if err := r.readBytes(clusterID[:]); err != nil {
		return nil, fmt.Errorf("wire: Commit cluster_id: %w", err)
	}
	opID, err := r.readUint64()
	if err != nil {
		return nil, fmt.Errorf("wire: Commit operator_id: %w", err)
	}
	height, err := r.readUint64()
	if err != nil {
		return nil, fmt.Errorf("wire: Commit height: %w", err)
	}
	sideByte, err := r.readByte()
	if err != nil {
		return nil, fmt.Errorf("wire: Commit side: %w", err)
	}
	side := twoab.CommitSide(sideByte)
	switch side {
	case twoab.CommitSideNR, twoab.CommitSideNRDirect:
		// valid post Op5
	default:
		// 0x01 was the pre-Op5 CommitSideSigned; deliberately rejected
		// here to make wire-version drift visible. ValidateCommit at the
		// Instance layer also rejects it.
		return nil, fmt.Errorf("wire: Commit side 0x%02x is invalid (post Op5; 0x01 Signed removed)", sideByte)
	}
	l0Value, err := r.readLengthPrefixed("Commit L0Value")
	if err != nil {
		return nil, err
	}
	l0Partial, err := r.readLengthPrefixed("Commit L0Partial")
	if err != nil {
		return nil, err
	}
	entries, err := decodeLayerEntries(r, "Commit")
	if err != nil {
		return nil, err
	}
	if err := r.requireEOF("Commit"); err != nil {
		return nil, err
	}
	return &twoab.Commit{
		ClusterID:    clusterID,
		OperatorID:   twoab.OperatorID(opID),
		Height:       twoab.Height(height),
		Side:         side,
		L0Value:      twoab.Value(l0Value),
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
		if len(e.V) > MaxFieldSize {
			return fmt.Errorf("wire: %s LayerEntries[%d] V too long (%d)", kindLabel, i, len(e.V))
		}
		if len(e.Payload) > MaxFieldSize {
			return fmt.Errorf("wire: %s LayerEntries[%d] Payload too long (%d)", kindLabel, i, len(e.Payload))
		}
	}
	return nil
}

func encodeLayerEntries(out []byte, entries []twoab.LayerEntry) []byte {
	out = appendUint32(out, uint32(len(entries))) //nolint:gosec // bounds-checked by preflight
	for _, e := range entries {
		out = appendUint32(out, uint32(e.Layer)) //nolint:gosec // bounds-checked by preflight
		out = append(out, byte(e.Kind))
		out = appendUint32(out, uint32(len(e.V))) //nolint:gosec // bounds-checked by preflight
		out = append(out, e.V...)
		out = appendUint32(out, uint32(len(e.Payload))) //nolint:gosec // bounds-checked by preflight
		out = append(out, e.Payload...)
	}
	return out
}

func decodeLayerEntries(r *reader, kindLabel string) ([]twoab.LayerEntry, error) {
	count, err := r.readUint32()
	if err != nil {
		return nil, fmt.Errorf("wire: %s LayerEntries count: %w", kindLabel, err)
	}
	if count > MaxLayers {
		return nil, fmt.Errorf("wire: %s LayerEntries count %d exceeds MaxLayers %d",
			kindLabel, count, MaxLayers)
	}
	entries := make([]twoab.LayerEntry, count)
	for i := uint32(0); i < count; i++ {
		layer, err := r.readUint32()
		if err != nil {
			return nil, fmt.Errorf("wire: %s LayerEntries[%d] layer: %w", kindLabel, i, err)
		}
		if layer > MaxLayers {
			return nil, fmt.Errorf("wire: %s LayerEntries[%d] layer %d exceeds MaxLayers %d",
				kindLabel, i, layer, MaxLayers)
		}
		kindByte, err := r.readByte()
		if err != nil {
			return nil, fmt.Errorf("wire: %s LayerEntries[%d] kind: %w", kindLabel, i, err)
		}
		kind := twoab.LayerEntryKind(kindByte)
		switch kind {
		case twoab.LayerEntryEmpty, twoab.LayerEntrySigmaChained, twoab.LayerEntryNRPlaintext:
			// valid
		default:
			return nil, fmt.Errorf("wire: %s LayerEntries[%d] kind 0x%02x is invalid",
				kindLabel, i, kindByte)
		}
		v, err := r.readLengthPrefixed(fmt.Sprintf("%s LayerEntries[%d] V", kindLabel, i))
		if err != nil {
			return nil, err
		}
		payload, err := r.readLengthPrefixed(fmt.Sprintf("%s LayerEntries[%d] Payload", kindLabel, i))
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
