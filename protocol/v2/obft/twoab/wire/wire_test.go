package wire

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ssvlabs/ssv/protocol/v2/obft/base"
	basewire "github.com/ssvlabs/ssv/protocol/v2/obft/base/wire"
	"github.com/ssvlabs/ssv/protocol/v2/obft/twoab"
)

// ---------- Phase1Bundle roundtrip ----------

func TestPhase1Bundle_Roundtrip(t *testing.T) {
	orig := &twoab.Phase1Bundle{
		ClusterID:  [32]byte{0xab, 0xcd, 0xef},
		OperatorID: twoab.OperatorID(7),
		Height:     twoab.Height(1234567),
		Layer:      2,
		Value:      twoab.Value("the-block-bytes"),
	}
	data, err := WrapPhase1Bundle(orig)
	require.NoError(t, err)

	env, err := Unwrap(data)
	require.NoError(t, err)
	require.Equal(t, KindPhase1Bundle, env.Kind)
	require.NotNil(t, env.Phase1Bundle)
	require.Equal(t, orig.ClusterID, env.Phase1Bundle.ClusterID)
	require.Equal(t, orig.OperatorID, env.Phase1Bundle.OperatorID)
	require.Equal(t, orig.Height, env.Phase1Bundle.Height)
	require.Equal(t, orig.Layer, env.Phase1Bundle.Layer)
	require.Equal(t, orig.Value, env.Phase1Bundle.Value)
}

func TestPhase1Bundle_Roundtrip_EmptyValue(t *testing.T) {
	// Validate rejects empty values, but the wire codec is lower-level; it
	// should round-trip empty values without error (validation layer is
	// separate).
	orig := &twoab.Phase1Bundle{
		ClusterID:  [32]byte{1},
		OperatorID: 1,
		Height:     1,
		Layer:      0,
		Value:      []byte{},
	}
	data, err := WrapPhase1Bundle(orig)
	require.NoError(t, err)
	env, err := Unwrap(data)
	require.NoError(t, err)
	require.Equal(t, KindPhase1Bundle, env.Kind)
	require.Empty(t, env.Phase1Bundle.Value)
}

func TestPhase1Bundle_RejectsNegativeLayer(t *testing.T) {
	orig := &twoab.Phase1Bundle{Layer: -1}
	_, err := EncodePhase1Bundle(orig)
	require.ErrorContains(t, err, "negative layer")
}

// ---------- Verdict roundtrip ----------

func TestVerdict_Roundtrip_SigmaV(t *testing.T) {
	valueRoot := [32]byte{0xfe, 0xed}
	orig := &twoab.Verdict{
		ClusterID:  [32]byte{0xab},
		OperatorID: 3,
		Height:     999,
		Layer:      1,
		Kind:       twoab.VerdictSigmaV,
		ValueRoot:  valueRoot,
	}
	data, err := WrapVerdict(orig)
	require.NoError(t, err)
	env, err := Unwrap(data)
	require.NoError(t, err)
	require.Equal(t, KindVerdict, env.Kind)
	require.Equal(t, *orig, *env.Verdict)
}

func TestVerdict_Roundtrip_NR(t *testing.T) {
	orig := &twoab.Verdict{
		ClusterID:  [32]byte{1},
		OperatorID: 2,
		Height:     1,
		Layer:      0,
		Kind:       twoab.VerdictNR,
		ValueRoot:  [32]byte{}, // null for NR
	}
	data, err := WrapVerdict(orig)
	require.NoError(t, err)
	env, err := Unwrap(data)
	require.NoError(t, err)
	require.Equal(t, *orig, *env.Verdict)
}

func TestVerdict_Roundtrip_NV(t *testing.T) {
	orig := &twoab.Verdict{
		ClusterID:  [32]byte{1},
		OperatorID: 2,
		Height:     1,
		Layer:      0,
		Kind:       twoab.VerdictNV,
		ValueRoot:  [32]byte{},
	}
	data, err := WrapVerdict(orig)
	require.NoError(t, err)
	env, err := Unwrap(data)
	require.NoError(t, err)
	require.Equal(t, *orig, *env.Verdict)
}

func TestVerdict_RejectsUnspecifiedKind(t *testing.T) {
	orig := &twoab.Verdict{Kind: twoab.VerdictUnspecified}
	_, err := EncodeVerdict(orig)
	require.ErrorContains(t, err, "unspecified")
}

func TestVerdict_DecodeRejectsInvalidKind(t *testing.T) {
	// Construct a valid envelope, then patch the kind byte to an invalid
	// value. The kind byte sits at offset 1 (version) + 16 (ProtocolTag) +
	// 1 (inner kind) + 32 (ClusterID) + 8 (OperatorID) + 8 (Height) +
	// 4 (Layer) = 70.
	orig := &twoab.Verdict{
		ClusterID:  [32]byte{1},
		OperatorID: 1,
		Height:     1,
		Layer:      0,
		Kind:       twoab.VerdictSigmaV,
		ValueRoot:  [32]byte{2},
	}
	body, err := EncodeVerdict(orig)
	require.NoError(t, err)
	// body[70] is the verdict-kind byte.
	body[70] = 0x99 // out-of-range kind value
	_, err = DecodeVerdict(body)
	require.ErrorContains(t, err, "kind 0x99 is invalid")
}

// ---------- Onion2b roundtrip ----------

func TestOnion2b_Roundtrip_Healthy(t *testing.T) {
	orig := &twoab.Onion2b{
		ClusterID:  [32]byte{0xab},
		OperatorID: 4,
		Height:     42,
		Layers: []twoab.EncryptedLayer{
			{Value: twoab.Value("V0"), Ciphertext: []byte("σ_0")},
			{Value: nil, Ciphertext: nil}, // operator did not σ-emit at L_1
			{Value: twoab.Value("V2"), Ciphertext: []byte("E(σ_2)")},
			{Value: nil, Ciphertext: nil},
		},
		NRPartials: []twoab.NRPartial{
			{Layer: 1, PartialSig: twoab.Signature("nr_1_partial")},
		},
	}
	data, err := WrapOnion2b(orig)
	require.NoError(t, err)
	env, err := Unwrap(data)
	require.NoError(t, err)
	require.Equal(t, KindOnion2b, env.Kind)
	got := env.Onion2b
	require.Equal(t, orig.ClusterID, got.ClusterID)
	require.Equal(t, orig.OperatorID, got.OperatorID)
	require.Equal(t, orig.Height, got.Height)
	require.Len(t, got.Layers, 4)
	require.Equal(t, orig.Layers[0].Value, got.Layers[0].Value)
	require.Equal(t, orig.Layers[0].Ciphertext, got.Layers[0].Ciphertext)
	// Empty entries roundtrip as zero-length (not nil).
	require.Empty(t, got.Layers[1].Value)
	require.Empty(t, got.Layers[1].Ciphertext)
	require.Equal(t, orig.Layers[2].Value, got.Layers[2].Value)
	require.Equal(t, orig.Layers[2].Ciphertext, got.Layers[2].Ciphertext)
	require.Len(t, got.NRPartials, 1)
	require.Equal(t, orig.NRPartials[0].Layer, got.NRPartials[0].Layer)
	require.Equal(t, orig.NRPartials[0].PartialSig, got.NRPartials[0].PartialSig)
}

func TestOnion2b_Roundtrip_AllNR(t *testing.T) {
	orig := &twoab.Onion2b{
		ClusterID:  [32]byte{1},
		OperatorID: 1,
		Height:     1,
		Layers:     []twoab.EncryptedLayer{{}, {}, {}, {}}, // no σ at any layer
		NRPartials: []twoab.NRPartial{
			{Layer: 0, PartialSig: twoab.Signature("nr0")},
			{Layer: 1, PartialSig: twoab.Signature("nr1")},
			{Layer: 2, PartialSig: twoab.Signature("nr2")},
		},
	}
	data, err := WrapOnion2b(orig)
	require.NoError(t, err)
	env, err := Unwrap(data)
	require.NoError(t, err)
	require.Len(t, env.Onion2b.Layers, 4)
	require.Len(t, env.Onion2b.NRPartials, 3)
}

func TestOnion2b_RejectsTooManyLayers(t *testing.T) {
	orig := &twoab.Onion2b{
		Layers: make([]twoab.EncryptedLayer, MaxLayers+1),
	}
	_, err := EncodeOnion2b(orig)
	require.ErrorContains(t, err, "max")
}

// ---------- Certificate roundtrip ----------

func TestCertificate_Roundtrip(t *testing.T) {
	orig := &twoab.Certificate{
		ClusterID: [32]byte{0xab},
		Height:    42,
		Value:     twoab.Value("the-final-V"),
		Signature: twoab.Signature("aggregate-bls-sig"),
	}
	data, err := WrapCertificate(orig)
	require.NoError(t, err)
	env, err := Unwrap(data)
	require.NoError(t, err)
	require.Equal(t, KindCertificate, env.Kind)
	require.Equal(t, *orig, *env.Certificate)
}

// ---------- ProtocolTag / version errors ----------

func TestUnwrap_RejectsTruncated(t *testing.T) {
	_, err := Unwrap([]byte{})
	require.Error(t, err)
}

func TestDecode_RejectsTrailingBytes(t *testing.T) {
	orig := &twoab.Verdict{
		ClusterID:  [32]byte{1},
		OperatorID: 1,
		Height:     1,
		Kind:       twoab.VerdictNR,
	}
	body, err := EncodeVerdict(orig)
	require.NoError(t, err)
	body = append(body, 0xff) // trailing junk
	_, err = DecodeVerdict(body)
	require.ErrorContains(t, err, "trailing")
}

func TestDecode_RejectsWrongInnerKind(t *testing.T) {
	// Encode a Phase1Bundle but decode as Verdict — inner-kind mismatch.
	b := &twoab.Phase1Bundle{
		ClusterID:  [32]byte{1},
		OperatorID: 1,
		Height:     1,
		Layer:      0,
		Value:      twoab.Value("V"),
	}
	body, err := EncodePhase1Bundle(b)
	require.NoError(t, err)
	_, err = DecodeVerdict(body)
	require.ErrorContains(t, err, "inner kind")
}

// ---------- Domain separation against bare-OBFT ----------

// A bare-OBFT-encoded Phase1Bundle must NOT decode as a twoab message: the
// ProtocolTag bytes differ ("OBFT-v1\0" vs "2abOBFT-v1\0\0\0\0\0\0"). This
// is the load-bearing cross-protocol-rejection guarantee from impl-plan G3.
func TestDomainSeparation_BareEncodeFailsAtTwoabDecode(t *testing.T) {
	bareBytes, err := basewire.WrapPhase1Bundle(&base.Phase1Bundle{
		ClusterID:  [32]byte{1},
		OperatorID: 1,
		Height:     1,
		Layer:      0,
		Value:      base.Value("V"),
		SigmaV:     base.Signature("sig"),
	})
	require.NoError(t, err)
	// Decoding bare bytes as twoab must fail. The envelope-frame layer
	// uses the same sharedwire.Frame, so Unframe succeeds; the failure
	// surfaces at body-decode time (ProtocolTag mismatch).
	_, err = Unwrap(bareBytes)
	require.Error(t, err)
	require.Contains(t, err.Error(), "protocol_tag")
}

// Inverse: a twoab-encoded Verdict must NOT decode as a bare-OBFT message.
// Bare OBFT doesn't have KindVerdict at all, but the byte value 0x02
// coincides with bare's KindCommit — so the bare envelope-kind dispatch
// will route the bytes to DecodeCommit, which fails on ProtocolTag.
func TestDomainSeparation_TwoabEncodeFailsAtBareDecode(t *testing.T) {
	verdict := &twoab.Verdict{
		ClusterID:  [32]byte{1},
		OperatorID: 1,
		Height:     1,
		Layer:      0,
		Kind:       twoab.VerdictNR,
	}
	twoabBytes, err := WrapVerdict(verdict)
	require.NoError(t, err)
	_, err = basewire.Unwrap(twoabBytes)
	require.Error(t, err)
}
