package twoab

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// Hash functions back Phase F (Rule 6a verdict-vs-verdict equivocation
// detection) and Phase G (cross-onion equivocation detection). They aren't
// invoked yet, but their correctness invariants are testable now:
//
//   1. Byte-identical re-broadcasts produce the same hash (dedup works).
//   2. Different content fields produce different hashes (false positives
//      → dedup-when-shouldnt → missed equivocation evidence).
//
// These tests pin those invariants down so Phase F/G can wire them up
// without re-litigating shape.

// ---------- verdictContentHash ----------

func TestVerdictContentHash_IdenticalInputsProduceIdenticalHashes(t *testing.T) {
	v1 := &Verdict{
		ClusterID:  [32]byte{0xab},
		OperatorID: 3,
		Height:     1234,
		Layer:      2,
		Kind:       VerdictSigmaV,
		ValueRoot:  [32]byte{0xfe, 0xed},
	}
	v2 := *v1 // value copy

	require.Equal(t, verdictContentHash(v1), verdictContentHash(&v2),
		"identical content → identical hash (dedup of byte-identical re-broadcasts)")
}

func TestVerdictContentHash_DistinguishesByOperator(t *testing.T) {
	base := &Verdict{
		ClusterID: [32]byte{0xab},
		Height:    1,
		Layer:     0,
		Kind:      VerdictNR,
	}
	v1 := *base
	v1.OperatorID = 1
	v2 := *base
	v2.OperatorID = 2
	require.NotEqual(t, verdictContentHash(&v1), verdictContentHash(&v2),
		"different operator → different hash")
}

func TestVerdictContentHash_DistinguishesByLayer(t *testing.T) {
	base := &Verdict{OperatorID: 1, Height: 1, Kind: VerdictNR}
	v1 := *base
	v1.Layer = 0
	v2 := *base
	v2.Layer = 1
	require.NotEqual(t, verdictContentHash(&v1), verdictContentHash(&v2))
}

func TestVerdictContentHash_DistinguishesByKind(t *testing.T) {
	// σV with non-zero root vs NR with zero root — different kinds.
	v1 := &Verdict{OperatorID: 1, Height: 1, Kind: VerdictSigmaV, ValueRoot: [32]byte{0xab}}
	v2 := &Verdict{OperatorID: 1, Height: 1, Kind: VerdictNR}
	require.NotEqual(t, verdictContentHash(v1), verdictContentHash(v2))
}

func TestVerdictContentHash_DistinguishesByValueRoot(t *testing.T) {
	v1 := &Verdict{OperatorID: 1, Height: 1, Kind: VerdictSigmaV, ValueRoot: [32]byte{0x01}}
	v2 := &Verdict{OperatorID: 1, Height: 1, Kind: VerdictSigmaV, ValueRoot: [32]byte{0x02}}
	require.NotEqual(t, verdictContentHash(v1), verdictContentHash(v2),
		"σV verdicts on different V's → different hashes (Rule-6a evidence input)")
}

func TestVerdictContentHash_DistinguishesByCluster(t *testing.T) {
	v1 := &Verdict{ClusterID: [32]byte{1}, OperatorID: 1, Kind: VerdictNR}
	v2 := &Verdict{ClusterID: [32]byte{2}, OperatorID: 1, Kind: VerdictNR}
	require.NotEqual(t, verdictContentHash(v1), verdictContentHash(v2))
}

func TestVerdictContentHash_DistinguishesByHeight(t *testing.T) {
	v1 := &Verdict{OperatorID: 1, Height: 1, Kind: VerdictNR}
	v2 := &Verdict{OperatorID: 1, Height: 2, Kind: VerdictNR}
	require.NotEqual(t, verdictContentHash(v1), verdictContentHash(v2))
}

// ---------- onion2bContentHash ----------

func TestOnion2bContentHash_IdenticalInputsProduceIdenticalHashes(t *testing.T) {
	o1 := &Onion2b{
		ClusterID:  [32]byte{0xab},
		OperatorID: 3,
		Height:     1234,
		Layers: []EncryptedLayer{
			{Value: Value("V0"), Ciphertext: []byte("σ0")},
			{},
		},
		NRPartials: []NRPartial{
			{Layer: 0, PartialSig: Signature("nr0")},
		},
	}
	o2 := *o1
	o2.Layers = append([]EncryptedLayer(nil), o1.Layers...)
	o2.NRPartials = append([]NRPartial(nil), o1.NRPartials...)
	require.Equal(t, onion2bContentHash(o1), onion2bContentHash(&o2),
		"identical content → identical hash")
}

func TestOnion2bContentHash_DistinguishesByLayerContent(t *testing.T) {
	// Two onions, same operator/cluster/height; different σ partial at L_0
	// → Rule-3 cross-onion equivocation candidate.
	o1 := &Onion2b{
		OperatorID: 1,
		Height:     1,
		Layers: []EncryptedLayer{
			{Value: Value("V"), Ciphertext: []byte("σ_a")},
		},
	}
	o2 := &Onion2b{
		OperatorID: 1,
		Height:     1,
		Layers: []EncryptedLayer{
			{Value: Value("V"), Ciphertext: []byte("σ_b")}, // different σ
		},
	}
	require.NotEqual(t, onion2bContentHash(o1), onion2bContentHash(o2),
		"different ciphertext at same layer → different hash")
}

func TestOnion2bContentHash_DistinguishesByValueAtLayer(t *testing.T) {
	// Two onions with σ partials on different V's at L_0 — direct Rule-3
	// equivocation evidence.
	o1 := &Onion2b{
		OperatorID: 1,
		Height:     1,
		Layers: []EncryptedLayer{
			{Value: Value("V_a"), Ciphertext: []byte("σ")},
		},
	}
	o2 := &Onion2b{
		OperatorID: 1,
		Height:     1,
		Layers: []EncryptedLayer{
			{Value: Value("V_b"), Ciphertext: []byte("σ")},
		},
	}
	require.NotEqual(t, onion2bContentHash(o1), onion2bContentHash(o2))
}

func TestOnion2bContentHash_DistinguishesByNRPartial(t *testing.T) {
	o1 := &Onion2b{
		OperatorID: 1, Height: 1,
		Layers: []EncryptedLayer{{}, {}},
		NRPartials: []NRPartial{
			{Layer: 0, PartialSig: Signature("a")},
		},
	}
	o2 := &Onion2b{
		OperatorID: 1, Height: 1,
		Layers: []EncryptedLayer{{}, {}},
		NRPartials: []NRPartial{
			{Layer: 0, PartialSig: Signature("b")},
		},
	}
	require.NotEqual(t, onion2bContentHash(o1), onion2bContentHash(o2),
		"different NR partial sig at same layer → different hash")
}

func TestOnion2bContentHash_DistinguishesByLayerCount(t *testing.T) {
	o1 := &Onion2b{OperatorID: 1, Layers: make([]EncryptedLayer, 3)}
	o2 := &Onion2b{OperatorID: 1, Layers: make([]EncryptedLayer, 4)}
	require.NotEqual(t, onion2bContentHash(o1), onion2bContentHash(o2))
}

// ---------- ValueRoot helper ----------

func TestValueRoot_DifferentVProducesDifferentRoot(t *testing.T) {
	r1 := ValueRoot(Value("V_a"))
	r2 := ValueRoot(Value("V_b"))
	require.NotEqual(t, r1, r2)
}

func TestValueRoot_SameVProducesSameRoot(t *testing.T) {
	r1 := ValueRoot(Value("V"))
	r2 := ValueRoot(Value("V"))
	require.Equal(t, r1, r2)
}

func TestValueRoot_EmptyV(t *testing.T) {
	// Edge case: ValueRoot(nil) is sha256(""), a well-defined fixed value.
	// (The protocol never carries empty V's through to verdicts — that's
	// caught at ValidateVerdict — but the function itself must be total.)
	r := ValueRoot(nil)
	require.NotEqual(t, [32]byte{}, r, "sha256 of empty input is non-zero")
}

// ---------- VerdictKind helpers ----------

func TestVerdictKind_String(t *testing.T) {
	require.Equal(t, "sigmaV", VerdictSigmaV.String())
	require.Equal(t, "NR", VerdictNR.String())
	require.Equal(t, "NV", VerdictNV.String())
	require.Equal(t, "unspecified", VerdictUnspecified.String())
	require.Equal(t, "unspecified", VerdictKind(0x42).String(), "unknown values render as unspecified")
}

func TestVerdictKind_IsNoSigmaSide(t *testing.T) {
	require.False(t, VerdictSigmaV.IsNoSigmaSide(), "σV is σ-side")
	require.True(t, VerdictNR.IsNoSigmaSide(), "NR counts toward nr_pool")
	require.True(t, VerdictNV.IsNoSigmaSide(), "NV counts toward nr_pool")
	require.False(t, VerdictUnspecified.IsNoSigmaSide(), "Unspecified contributes nothing")
}
