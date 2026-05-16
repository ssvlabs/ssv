package wire

import (
	"bytes"
	"testing"

	"github.com/stretchr/testify/require"

	base "github.com/ssvlabs/ssv/protocol/v2/obft/base"
)

func TestPhase1Bundle_Roundtrip(t *testing.T) {
	in := &base.Phase1Bundle{
		ClusterID:  [32]byte{0xAA, 0xBB, 0xCC, 0xDD},
		OperatorID: 7,
		Height:     12345,
		Layer:      2,
		Value:      []byte("hello world"),
		SigmaV:     []byte{0x01, 0x02, 0x03, 0x04},
	}
	bytes_, err := WrapPhase1Bundle(in)
	require.NoError(t, err)

	env, err := Unwrap(bytes_)
	require.NoError(t, err)
	require.Equal(t, KindPhase1Bundle, env.Kind)
	require.Equal(t, in.ClusterID, env.Phase1Bundle.ClusterID)
	require.Equal(t, in.OperatorID, env.Phase1Bundle.OperatorID)
	require.Equal(t, in.Height, env.Phase1Bundle.Height)
	require.Equal(t, in.Layer, env.Phase1Bundle.Layer)
	require.True(t, bytes.Equal(in.Value, env.Phase1Bundle.Value))
	require.True(t, bytes.Equal(in.SigmaV, env.Phase1Bundle.SigmaV))
}

func TestCommit_Roundtrip(t *testing.T) {
	in := &base.Commit{
		ClusterID:  [32]byte{0x11, 0x22, 0x33},
		OperatorID: 3,
		Height:     999,
		Layers: []base.EncryptedLayer{
			{Value: []byte("V0"), Ciphertext: []byte("ct0")},
			{}, // empty (no σ contribution)
			{Value: []byte("V2"), Ciphertext: []byte("ciphertext-for-layer-2")},
			{Value: []byte("V3-deepest"), Ciphertext: []byte("CT")},
		},
		NRPartials: []base.NRPartial{
			{Layer: 1, PartialSig: []byte("nr-sig-layer-1")},
		},
		Witnesses: []base.LeaderSigmaWitness{
			{Layer: 0, Leader: 1, ValueRoot: base.ValueRoot([]byte("V0")), SigmaV: []byte("sigma-from-leader-1")},
			{Layer: 2, Leader: 3, ValueRoot: base.ValueRoot([]byte("V2")), SigmaV: []byte("sigma-from-leader-3")},
		},
	}
	bytes_, err := WrapCommit(in)
	require.NoError(t, err)

	env, err := Unwrap(bytes_)
	require.NoError(t, err)
	require.Equal(t, KindCommit, env.Kind)
	require.Equal(t, in.ClusterID, env.Commit.ClusterID)
	require.Equal(t, in.OperatorID, env.Commit.OperatorID)
	require.Equal(t, in.Height, env.Commit.Height)
	require.Len(t, env.Commit.Layers, len(in.Layers))
	for i, expected := range in.Layers {
		got := env.Commit.Layers[i]
		require.True(t, bytes.Equal(expected.Value, got.Value), "layer %d Value mismatch", i)
		require.True(t, bytes.Equal(expected.Ciphertext, got.Ciphertext), "layer %d Ciphertext mismatch", i)
	}
	require.Len(t, env.Commit.NRPartials, len(in.NRPartials))
	for i, expected := range in.NRPartials {
		got := env.Commit.NRPartials[i]
		require.Equal(t, expected.Layer, got.Layer)
		require.True(t, bytes.Equal(expected.PartialSig, got.PartialSig))
	}
	require.Len(t, env.Commit.Witnesses, len(in.Witnesses))
	for i, expected := range in.Witnesses {
		got := env.Commit.Witnesses[i]
		require.Equal(t, expected.Layer, got.Layer)
		require.Equal(t, expected.Leader, got.Leader)
		require.Equal(t, expected.ValueRoot, got.ValueRoot, "witness %d ValueRoot mismatch", i)
		require.True(t, bytes.Equal(expected.SigmaV, got.SigmaV), "witness %d SigmaV mismatch", i)
	}
}

func TestCommit_Roundtrip_NoWitnesses(t *testing.T) {
	// A commit with no witnesses still encodes/decodes (witness count = 0).
	in := &base.Commit{
		OperatorID: 1,
		Height:     1,
		Layers:     []base.EncryptedLayer{{Value: []byte("V"), Ciphertext: []byte("c")}},
	}
	b, err := EncodeCommit(in)
	require.NoError(t, err)
	out, err := DecodeCommit(b)
	require.NoError(t, err)
	require.Empty(t, out.Witnesses)
}

func TestEncodeCommit_RejectsTooManyWitnesses(t *testing.T) {
	c := &base.Commit{
		Layers:    []base.EncryptedLayer{{Value: []byte("V"), Ciphertext: []byte("c")}},
		Witnesses: make([]base.LeaderSigmaWitness, MaxWitnesses+1),
	}
	_, err := EncodeCommit(c)
	require.ErrorContains(t, err, "max")
}

// ---- G3: wire-decode bounds checks on layer indices ----

func TestDecodePhase1Bundle_RejectsLayerExceedingMaxLayers(t *testing.T) {
	// Build a syntactically valid bundle with layer = MaxLayers+1.
	in := &base.Phase1Bundle{
		OperatorID: 1, Height: 1, Layer: 0,
		Value: []byte("V"), SigmaV: []byte("s"),
	}
	encoded, err := EncodePhase1Bundle(in)
	require.NoError(t, err)
	// Patch the encoded layer field directly. Layout:
	// [1 ver][8 tag][1 inner_kind][32 cluster][8 op][8 h][4 layer]...
	// → layer at offset 1+8+1+32+8+8 = 58.
	encoded[58] = 0xFF
	encoded[59] = 0xFF
	encoded[60] = 0xFF
	encoded[61] = 0xFF
	_, err = DecodePhase1Bundle(encoded)
	require.ErrorContains(t, err, "exceeds MaxLayers")
}

// ---- Auth envelope: ProtocolTag + inner_kind binding ----

func TestDecodePhase1Bundle_RejectsBadProtocolTag(t *testing.T) {
	in := &base.Phase1Bundle{
		OperatorID: 1, Height: 1, Layer: 0,
		Value: []byte("V"), SigmaV: []byte("s"),
	}
	encoded, err := EncodePhase1Bundle(in)
	require.NoError(t, err)
	// ProtocolTag is at offset 1 (right after version). Corrupt the first byte.
	encoded[1] = 'X'
	_, err = DecodePhase1Bundle(encoded)
	require.ErrorContains(t, err, "protocol tag mismatch")
}

func TestDecodePhase1Bundle_RejectsBadInnerKind(t *testing.T) {
	in := &base.Phase1Bundle{
		OperatorID: 1, Height: 1, Layer: 0,
		Value: []byte("V"), SigmaV: []byte("s"),
	}
	encoded, err := EncodePhase1Bundle(in)
	require.NoError(t, err)
	// Inner kind is at offset 1+8 = 9. Set it to commit's kind (0x02).
	encoded[9] = 0x02
	_, err = DecodePhase1Bundle(encoded)
	require.ErrorContains(t, err, "inner kind")
}

func TestDecodeCommit_RejectsBadInnerKind(t *testing.T) {
	in := &base.Commit{
		OperatorID: 1, Height: 1,
		Layers: []base.EncryptedLayer{{Value: []byte("V"), Ciphertext: []byte("c")}},
	}
	encoded, err := EncodeCommit(in)
	require.NoError(t, err)
	// Inner kind at offset 9. Set to certificate's kind (0x03).
	encoded[9] = 0x03
	_, err = DecodeCommit(encoded)
	require.ErrorContains(t, err, "inner kind")
}

func TestDecodeCertificate_RejectsBadProtocolTag(t *testing.T) {
	in := &base.Certificate{
		Height:    1,
		Value:     []byte("V"),
		Signature: []byte("s"),
	}
	encoded, err := EncodeCertificate(in)
	require.NoError(t, err)
	encoded[3] = 'X'
	_, err = DecodeCertificate(encoded)
	require.ErrorContains(t, err, "protocol tag mismatch")
}

func TestDecodeCommit_RejectsNRPartialLayerExceedingMaxLayers(t *testing.T) {
	// Manually craft a commit with a single NR partial whose layer is huge.
	c := &base.Commit{
		OperatorID: 1, Height: 1,
		Layers: []base.EncryptedLayer{{Value: []byte("V"), Ciphertext: []byte("c")}},
		NRPartials: []base.NRPartial{
			{Layer: 0, PartialSig: []byte("sig")},
		},
	}
	encoded, err := EncodeCommit(c)
	require.NoError(t, err)
	// Layout: [1 ver][8 tag][1 inner_kind][32 cluster][8 op][8 h][2 layer count] = 60 bytes header.
	// Then 1 layer with V="V" (4+1) + ciphertext "c" (4+1) = 10 bytes → 70.
	// Then NR count uint16 at 70, then NR partial layer uint32 at 72.
	encoded[72] = 0x00
	encoded[73] = 0x00
	encoded[74] = byte((MaxLayers + 1) >> 8)
	encoded[75] = byte((MaxLayers + 1) & 0xFF)
	_, err = DecodeCommit(encoded)
	require.ErrorContains(t, err, "exceeds MaxLayers")
}

func TestCertificate_Roundtrip(t *testing.T) {
	in := &base.Certificate{
		ClusterID: [32]byte{0xCA, 0xFE, 0xBA, 0xBE},
		Height:    77,
		Value:     []byte("decided value bytes"),
		Signature: []byte{0xAA, 0xBB, 0xCC, 0xDD, 0xEE, 0xFF},
	}
	bytes_, err := WrapCertificate(in)
	require.NoError(t, err)

	env, err := Unwrap(bytes_)
	require.NoError(t, err)
	require.Equal(t, KindCertificate, env.Kind)
	require.Equal(t, in.ClusterID, env.Certificate.ClusterID)
	require.Equal(t, in.Height, env.Certificate.Height)
	require.True(t, bytes.Equal(in.Value, env.Certificate.Value))
	require.True(t, bytes.Equal(in.Signature, env.Certificate.Signature))
}

func TestUnwrap_RejectsUnknownKind(t *testing.T) {
	// Forge an envelope with an unknown kind byte.
	data := []byte{EnvelopeVersionV1, 0x99, 0x00, 0x01, 0x02}
	_, err := Unwrap(data)
	require.ErrorContains(t, err, "unknown envelope kind")
}

func TestUnwrap_RejectsTruncated(t *testing.T) {
	_, err := Unwrap([]byte{EnvelopeVersionV1})
	require.Error(t, err)
}

func TestDecodePhase1Bundle_RejectsUnknownVersion(t *testing.T) {
	bogus := []byte{0xFF, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0}
	_, err := DecodePhase1Bundle(bogus)
	require.ErrorContains(t, err, "unsupported phase-1 bundle version")
}

func TestEncodeCommit_RejectsTooManyLayers(t *testing.T) {
	c := &base.Commit{Layers: make([]base.EncryptedLayer, MaxLayers+1)}
	_, err := EncodeCommit(c)
	require.ErrorContains(t, err, "max")
}

// ---- per-field length caps (MaxValueSize / MaxSignatureSize / MaxCiphertextSize) ----

// TestEncodePhase1Bundle_RejectsOverlongValue confirms the encoder rejects a
// Value larger than MaxValueSize (defense against unbounded allocation by a
// malformed sender). MaxValueSize is the proposer-duty Value cap.
func TestEncodePhase1Bundle_RejectsOverlongValue(t *testing.T) {
	b := &base.Phase1Bundle{
		OperatorID: 1, Height: 1, Layer: 0,
		Value:  make([]byte, MaxValueSize+1),
		SigmaV: []byte("s"),
	}
	_, err := EncodePhase1Bundle(b)
	require.ErrorContains(t, err, "too long")
}

// TestEncodePhase1Bundle_RejectsOverlongSigmaV confirms the encoder rejects a
// SigmaV larger than MaxSignatureSize (signatures are tighter-capped than
// values).
func TestEncodePhase1Bundle_RejectsOverlongSigmaV(t *testing.T) {
	b := &base.Phase1Bundle{
		OperatorID: 1, Height: 1, Layer: 0,
		Value:  []byte("V"),
		SigmaV: make([]byte, MaxSignatureSize+1),
	}
	_, err := EncodePhase1Bundle(b)
	require.ErrorContains(t, err, "too long")
}

// TestEncodeCommit_RejectsOverlongCiphertext confirms ciphertext fields cap
// at MaxCiphertextSize (chained-IBE wrapped signatures are tighter-capped
// than full Values).
func TestEncodeCommit_RejectsOverlongCiphertext(t *testing.T) {
	c := &base.Commit{
		Layers: []base.EncryptedLayer{{
			Value:      []byte("V"),
			Ciphertext: make([]byte, MaxCiphertextSize+1),
		}},
	}
	_, err := EncodeCommit(c)
	require.ErrorContains(t, err, "too long")
}

// TestEncodeCommit_RejectsOverlongNRPartialSig confirms NR-partial-sig fields
// cap at MaxSignatureSize.
func TestEncodeCommit_RejectsOverlongNRPartialSig(t *testing.T) {
	c := &base.Commit{
		NRPartials: []base.NRPartial{{
			Layer:      0,
			PartialSig: make([]byte, MaxSignatureSize+1),
		}},
	}
	_, err := EncodeCommit(c)
	require.ErrorContains(t, err, "too long")
}

// TestEncodeCommit_RejectsOverlongWitnessSigma confirms witness SigmaV fields
// cap at MaxSignatureSize.
func TestEncodeCommit_RejectsOverlongWitnessSigma(t *testing.T) {
	c := &base.Commit{
		Witnesses: []base.LeaderSigmaWitness{{
			Layer:  0,
			Leader: 1,
			SigmaV: make([]byte, MaxSignatureSize+1),
		}},
	}
	_, err := EncodeCommit(c)
	require.ErrorContains(t, err, "too long")
}

// TestEncodeCertificate_RejectsOverlongFields exercises both cert-field caps.
func TestEncodeCertificate_RejectsOverlongFields(t *testing.T) {
	t.Run("value", func(t *testing.T) {
		c := &base.Certificate{
			Value:     make([]byte, MaxValueSize+1),
			Signature: []byte("s"),
		}
		_, err := EncodeCertificate(c)
		require.ErrorContains(t, err, "too long")
	})
	t.Run("signature", func(t *testing.T) {
		c := &base.Certificate{
			Value:     []byte("V"),
			Signature: make([]byte, MaxSignatureSize+1),
		}
		_, err := EncodeCertificate(c)
		require.ErrorContains(t, err, "too long")
	})
}
