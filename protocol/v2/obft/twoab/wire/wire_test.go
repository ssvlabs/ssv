package wire

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ssvlabs/ssv/protocol/v2/obft/twoab"
)

func TestProtocolTag_IsTwoabOBFTNotV1(t *testing.T) {
	// ProtocolTag is "2abOBFT" + 9 NUL bytes per the rewrite (no "-v1" suffix).
	expected := [16]byte{'2', 'a', 'b', 'O', 'B', 'F', 'T'}
	require.Equal(t, expected, ProtocolTag)
}

func TestPhase1Bundle_EncodeDecodeRoundTrip(t *testing.T) {
	b := &twoab.Phase1Bundle{
		ClusterID:  [32]byte{0xaa, 0xbb, 0xcc},
		OperatorID: 1,
		Height:     42,
		Layer:      0,
		Value:      twoab.Value("V0-bytes"),
		LWitness:   twoab.Signature{0xde, 0xad, 0xbe, 0xef, 0x01, 0x02, 0x03, 0x04},
	}
	encoded, err := EncodePhase1Bundle(b)
	require.NoError(t, err)
	decoded, err := DecodePhase1Bundle(encoded)
	require.NoError(t, err)
	require.Equal(t, b.ClusterID, decoded.ClusterID)
	require.Equal(t, b.OperatorID, decoded.OperatorID)
	require.Equal(t, b.Height, decoded.Height)
	require.Equal(t, b.Layer, decoded.Layer)
	require.Equal(t, b.Value, decoded.Value)
	require.Equal(t, b.LWitness, decoded.LWitness,
		"Op8 LWitness must round-trip through wire encode/decode")
}

// TestPhase1Bundle_RejectsUnknownVersionByte verifies that the V3
// decoder rejects wire bytes whose version byte differs from 0x03.
// Includes rejection of the pre-Op8 V2 byte (0x02) and the pre-Op3 V1
// byte (0x01) — the cluster cutover policy assumes wire-incompatible
// coexistence does not occur, so older-version bytes hitting a V3 decoder
// should fail cleanly at the version check (manifesting as silent quorum
// starvation on mixed-version clusters — see wire.go's cluster-cutover
// documentation).
func TestPhase1Bundle_RejectsUnknownVersionByte(t *testing.T) {
	for _, badVersion := range []byte{0x00, 0x01, 0x02, 0xff} {
		bytes := []byte{badVersion}
		bytes = append(bytes, ProtocolTag[:]...)
		bytes = append(bytes, 0x01) // inner kind Phase1Bundle
		_, err := DecodePhase1Bundle(bytes)
		require.Errorf(t, err, "V3 decoder must reject version byte 0x%02x", badVersion)
	}
}

// TestPhase1Bundle_EncodeDecodeRoundTrip_EmptyLWitness verifies that the
// wire encoding tolerates an empty LWitness at the encoder/decoder level
// (the non-empty check is at ValidatePhase1Bundle — separation of
// concerns). Post-Op8 ValidatePhase1Bundle rejects an empty witness at
// every layer, but the codec itself stays agnostic so a malformed bundle
// fails at validation (with a specific error) rather than at decode.
func TestPhase1Bundle_EncodeDecodeRoundTrip_EmptyLWitness(t *testing.T) {
	b := &twoab.Phase1Bundle{
		ClusterID:  [32]byte{0xaa},
		OperatorID: 2,
		Height:     1,
		Layer:      1,
		Value:      twoab.Value("V1"),
		// LWitness intentionally empty — codec tolerates; validation rejects.
	}
	encoded, err := EncodePhase1Bundle(b)
	require.NoError(t, err)
	decoded, err := DecodePhase1Bundle(encoded)
	require.NoError(t, err)
	require.Empty(t, decoded.LWitness)
}

func TestValueMsg_EncodeDecodeRoundTrip(t *testing.T) {
	v := twoab.Value("V0-bytes")
	vm := &twoab.ValueMsg{
		ClusterID:  [32]byte{0xaa},
		OperatorID: 2,
		Height:     5,
		V:          v,
		ValueRoot:  twoab.ValueRoot(v),
		// Op12: a Layer-0 witness plus a deeper-layer one (exercises the
		// multi-entry Witnesses[] list codec).
		Witnesses: []twoab.LayerWitness{
			{Layer: 0, ValueRoot: twoab.ValueRoot(v), Witness: twoab.Signature{0xde, 0xad, 0xbe, 0xef, 0x01, 0x02, 0x03, 0x04}},
			{Layer: 1, ValueRoot: twoab.ValueRoot(twoab.Value("V1")), Witness: twoab.Signature{0x11, 0x22, 0x33}},
		},
		L0Partial: twoab.Signature{0x10, 0x20, 0x30, 0x40, 0x50, 0x60, 0x70, 0x80},
		LayerEntries: []twoab.LayerEntry{
			{Layer: 1, Kind: twoab.LayerEntrySigmaChained, V: twoab.Value("V1"), Payload: []byte("ct")},
		},
	}
	encoded, err := EncodeValueMsg(vm)
	require.NoError(t, err)
	decoded, err := DecodeValueMsg(encoded)
	require.NoError(t, err)
	require.Equal(t, vm.ClusterID, decoded.ClusterID)
	require.Equal(t, vm.OperatorID, decoded.OperatorID)
	require.Equal(t, vm.V, decoded.V)
	require.Equal(t, vm.ValueRoot, decoded.ValueRoot)
	require.Equal(t, vm.Witnesses, decoded.Witnesses,
		"Op12: KindValue Witnesses[] must round-trip through wire encode/decode")
	require.Equal(t, vm.L0Partial, decoded.L0Partial,
		"Op5: KindValue L0Partial must round-trip through wire encode/decode")
	require.Len(t, decoded.LayerEntries, 1)
	require.Equal(t, vm.LayerEntries[0].Layer, decoded.LayerEntries[0].Layer)
	require.Equal(t, vm.LayerEntries[0].Kind, decoded.LayerEntries[0].Kind)
}

// TestValueMsg_RejectsUnknownVersionByte verifies that the V4 decoder
// rejects wire bytes whose version byte differs from 0x04. Includes
// rejection of the pre-Op12 V3 byte (0x03), the pre-Op5 V2 byte (0x02),
// and the pre-Op11 V1 byte (0x01) — the cluster cutover policy assumes
// wire-incompatible coexistence does not occur, so older-version bytes
// hitting a V4 decoder should fail cleanly at the version check.
func TestValueMsg_RejectsUnknownVersionByte(t *testing.T) {
	for _, badVersion := range []byte{0x00, 0x01, 0x02, 0x03, 0xff} {
		bytes := []byte{badVersion}
		bytes = append(bytes, ProtocolTag[:]...)
		bytes = append(bytes, 0x02) // inner kind ValueMsg
		_, err := DecodeValueMsg(bytes)
		require.Errorf(t, err, "V4 decoder must reject version byte 0x%02x", badVersion)
	}
}

func TestNoValueMsg_EncodeDecodeRoundTrip(t *testing.T) {
	nv := &twoab.NoValueMsg{
		ClusterID:  [32]byte{0xbb},
		OperatorID: 3,
		Height:     7,
		LayerEntries: []twoab.LayerEntry{
			{Layer: 1, Kind: twoab.LayerEntryNRPlaintext, Payload: []byte("nr-partial")},
		},
	}
	encoded, err := EncodeNoValueMsg(nv)
	require.NoError(t, err)
	decoded, err := DecodeNoValueMsg(encoded)
	require.NoError(t, err)
	require.Equal(t, nv.OperatorID, decoded.OperatorID)
	require.Len(t, decoded.LayerEntries, 1)
	require.Equal(t, twoab.LayerEntryNRPlaintext, decoded.LayerEntries[0].Kind)
}

// TestCommit_NREncodeDecodeRoundTrip covers the Phase-2b NR commit
// (KindCommit-NR) wire round-trip. Post Op5, this is the most common
// Commit kind on the wire — KindCommit-Signed has been removed and the
// σ-side terminal emission moved into KindValue.
func TestCommit_NREncodeDecodeRoundTrip(t *testing.T) {
	c := &twoab.Commit{
		ClusterID:  [32]byte{0xcc},
		OperatorID: 1,
		Height:     9,
		Side:       twoab.CommitSideNR,
		L0Partial:  twoab.Signature("nr-partial-bytes"),
	}
	encoded, err := EncodeCommit(c)
	require.NoError(t, err)
	decoded, err := DecodeCommit(encoded)
	require.NoError(t, err)
	require.Equal(t, twoab.CommitSideNR, decoded.Side)
	require.Empty(t, decoded.L0Value)
	require.Equal(t, c.L0Partial, decoded.L0Partial)
	require.Empty(t, decoded.LayerEntries)
}

// TestCommit_SignedSideRejected verifies that the pre-Op5 Signed side
// (byte 0x01) is rejected by the decoder loudly, surfacing wire-version
// drift between cluster members.
func TestCommit_SignedSideRejected(t *testing.T) {
	// Craft a Commit byte stream with side byte 0x01 (pre-Op5 Signed).
	// Use Encode with a hand-stamped side to bypass our own constants.
	bytes := []byte{CommitVersionV1}
	bytes = append(bytes, ProtocolTag[:]...)
	bytes = append(bytes, 0x04)                // inner kind = Commit
	bytes = append(bytes, make([]byte, 32)...) // ClusterID
	bytes = append(bytes, make([]byte, 8)...)  // OperatorID
	bytes = append(bytes, make([]byte, 8)...)  // Height
	bytes = append(bytes, 0x01)                // side = Signed (removed post Op5)
	bytes = append(bytes, 0, 0, 0, 0)          // L0Value length = 0
	bytes = append(bytes, 0, 0, 0, 1, 0xaa)    // L0Partial length=1 + 1 byte
	bytes = append(bytes, 0, 0, 0, 0)          // LayerEntries count = 0
	_, err := DecodeCommit(bytes)
	require.Error(t, err, "Op5: pre-Op5 CommitSideSigned (0x01) must be rejected at wire decode")
}

func TestCommit_NRDirectEncodeDecodeRoundTrip(t *testing.T) {
	c := &twoab.Commit{
		ClusterID:  [32]byte{0xdd},
		OperatorID: 2,
		Height:     10,
		Side:       twoab.CommitSideNRDirect,
		L0Partial:  twoab.Signature("nr-partial"),
		LayerEntries: []twoab.LayerEntry{
			{Layer: 1, Kind: twoab.LayerEntryEmpty},
		},
	}
	encoded, err := EncodeCommit(c)
	require.NoError(t, err)
	decoded, err := DecodeCommit(encoded)
	require.NoError(t, err)
	require.Equal(t, twoab.CommitSideNRDirect, decoded.Side)
	require.Empty(t, decoded.L0Value)
	require.Len(t, decoded.LayerEntries, 1)
}

func TestCertificate_EncodeDecodeRoundTrip(t *testing.T) {
	c := &twoab.Certificate{
		ClusterID: [32]byte{0xee},
		Height:    11,
		Value:     twoab.Value("V"),
		Signature: twoab.Signature("aggsig"),
	}
	encoded, err := EncodeCertificate(c)
	require.NoError(t, err)
	decoded, err := DecodeCertificate(encoded)
	require.NoError(t, err)
	require.Equal(t, c.Value, decoded.Value)
	require.Equal(t, c.Signature, decoded.Signature)
}

func TestEnvelope_WrapUnwrapRoundTrip(t *testing.T) {
	b := &twoab.Phase1Bundle{
		ClusterID: [32]byte{0xaa}, OperatorID: 1, Height: 1, Layer: 0, Value: twoab.Value("V"),
	}
	encoded, err := WrapPhase1Bundle(b)
	require.NoError(t, err)
	env, err := Unwrap(encoded)
	require.NoError(t, err)
	require.Equal(t, KindPhase1Bundle, env.Kind)
	require.NotNil(t, env.Phase1Bundle)
}

func TestDomainSeparation_RejectsBareOBFTTag(t *testing.T) {
	// Construct a wire body that looks like a Phase1Bundle but with a
	// different ProtocolTag (simulating a bare-OBFT envelope being
	// mis-routed to the twoab wire layer).
	bogus := make([]byte, 0)
	bogus = append(bogus, Phase1BundleVersionV3)
	// Wrong tag — "OBFT" left-aligned.
	bogus = append(bogus, []byte{'O', 'B', 'F', 'T', 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0}...)
	bogus = append(bogus, 0x01) // inner kind = phase1
	bogus = append(bogus, make([]byte, 32+8+8+4+4+4)...)
	_, err := DecodePhase1Bundle(bogus)
	require.Error(t, err, "wrong ProtocolTag should be rejected")
}

func TestUnwrap_UnknownKindRejected(t *testing.T) {
	bogus := []byte{0x01, 0xff} // version + unknown kind
	bogus = append(bogus, make([]byte, 10)...)
	_, err := Unwrap(bogus)
	require.Error(t, err)
}
