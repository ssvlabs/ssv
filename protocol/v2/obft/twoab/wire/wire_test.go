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
}

func TestValueMsg_EncodeDecodeRoundTrip(t *testing.T) {
	v := twoab.Value("V0-bytes")
	vm := &twoab.ValueMsg{
		ClusterID:  [32]byte{0xaa},
		OperatorID: 2,
		Height:     5,
		V:          v,
		ValueRoot:  twoab.ValueRoot(v),
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
	require.Len(t, decoded.LayerEntries, 1)
	require.Equal(t, vm.LayerEntries[0].Layer, decoded.LayerEntries[0].Layer)
	require.Equal(t, vm.LayerEntries[0].Kind, decoded.LayerEntries[0].Kind)
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

func TestCommit_SignedEncodeDecodeRoundTrip(t *testing.T) {
	c := &twoab.Commit{
		ClusterID:  [32]byte{0xcc},
		OperatorID: 1,
		Height:     9,
		Side:       twoab.CommitSideSigned,
		L0Value:    twoab.Value("V0"),
		L0Partial:  twoab.Signature("partial-bytes"),
	}
	encoded, err := EncodeCommit(c)
	require.NoError(t, err)
	decoded, err := DecodeCommit(encoded)
	require.NoError(t, err)
	require.Equal(t, twoab.CommitSideSigned, decoded.Side)
	require.Equal(t, c.L0Value, decoded.L0Value)
	require.Equal(t, c.L0Partial, decoded.L0Partial)
	require.Empty(t, decoded.LayerEntries)
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
	bogus = append(bogus, Phase1BundleVersionV1)
	// Wrong tag — "OBFT" left-aligned.
	bogus = append(bogus, []byte{'O', 'B', 'F', 'T', 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0}...)
	bogus = append(bogus, 0x01) // inner kind = phase1
	bogus = append(bogus, make([]byte, 32+8+8+4+4)...)
	_, err := DecodePhase1Bundle(bogus)
	require.Error(t, err, "wrong ProtocolTag should be rejected")
}

func TestUnwrap_UnknownKindRejected(t *testing.T) {
	bogus := []byte{0x01, 0xff} // version + unknown kind
	bogus = append(bogus, make([]byte, 10)...)
	_, err := Unwrap(bogus)
	require.Error(t, err)
}
