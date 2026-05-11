package twoab

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func validPhase1Bundle(c *Config) *Phase1Bundle {
	return &Phase1Bundle{
		ClusterID:  c.ClusterID,
		OperatorID: c.Layers[0].Leader,
		Height:     c.Height,
		Layer:      0,
		Value:      Value("V0"),
	}
}

// ---------- ValidatePhase1Bundle ----------

func TestValidatePhase1Bundle_AcceptsHealthy(t *testing.T) {
	c := healthyConfig()
	require.NoError(t, ValidatePhase1Bundle(validPhase1Bundle(c), c))
}

func TestValidatePhase1Bundle_RejectsNil(t *testing.T) {
	c := healthyConfig()
	require.ErrorContains(t, ValidatePhase1Bundle(nil, c), "nil phase-1 bundle")
}

func TestValidatePhase1Bundle_RejectsNilConfig(t *testing.T) {
	require.ErrorContains(t, ValidatePhase1Bundle(&Phase1Bundle{}, nil), "nil config")
}

func TestValidatePhase1Bundle_RejectsClusterMismatch(t *testing.T) {
	c := healthyConfig()
	b := validPhase1Bundle(c)
	b.ClusterID = [32]byte{0xff} // different
	require.ErrorContains(t, ValidatePhase1Bundle(b, c), "cluster id")
}

func TestValidatePhase1Bundle_RejectsHeightMismatch(t *testing.T) {
	c := healthyConfig()
	b := validPhase1Bundle(c)
	b.Height = c.Height + 1
	require.ErrorContains(t, ValidatePhase1Bundle(b, c), "height")
}

func TestValidatePhase1Bundle_RejectsLayerOutOfRange(t *testing.T) {
	c := healthyConfig()
	b := validPhase1Bundle(c)
	b.Layer = c.K() // out of range
	require.ErrorContains(t, ValidatePhase1Bundle(b, c), "out of range")
}

func TestValidatePhase1Bundle_RejectsWrongLeader(t *testing.T) {
	c := healthyConfig()
	b := validPhase1Bundle(c)
	b.OperatorID = c.Layers[1].Leader // wrong leader for layer 0
	require.ErrorContains(t, ValidatePhase1Bundle(b, c), "layer 0's leader")
}

func TestValidatePhase1Bundle_RejectsEmptyValue(t *testing.T) {
	c := healthyConfig()
	b := validPhase1Bundle(c)
	b.Value = nil
	require.ErrorContains(t, ValidatePhase1Bundle(b, c), "empty Value")
}

// Spec note: 2abOBFT Phase-1 bundle has NO SigmaV field. Validation must
// not require one (regression guard against accidental re-introduction).
func TestValidatePhase1Bundle_NoSigmaVRequired(t *testing.T) {
	c := healthyConfig()
	// Even with an empty Value, Validate complains about Value, not SigmaV.
	// And the absence of SigmaV is the design — 2ab doesn't have it.
	b := validPhase1Bundle(c)
	require.NoError(t, ValidatePhase1Bundle(b, c))
}

// ---------- ValidateVerdict ----------

func validVerdict(c *Config) *Verdict {
	return &Verdict{
		ClusterID:  c.ClusterID,
		OperatorID: c.Operators[0],
		Height:     c.Height,
		Layer:      0,
		Kind:       VerdictSigmaV,
		ValueRoot:  [32]byte{0xab}, // non-zero for σV
	}
}

func TestValidateVerdict_AcceptsHealthy(t *testing.T) {
	c := healthyConfig()
	require.NoError(t, ValidateVerdict(validVerdict(c), c))
}

func TestValidateVerdict_AcceptsNR(t *testing.T) {
	c := healthyConfig()
	v := validVerdict(c)
	v.Kind = VerdictNR
	v.ValueRoot = [32]byte{} // null for NR
	require.NoError(t, ValidateVerdict(v, c))
}

func TestValidateVerdict_AcceptsNV(t *testing.T) {
	c := healthyConfig()
	v := validVerdict(c)
	v.Kind = VerdictNV
	v.ValueRoot = [32]byte{}
	require.NoError(t, ValidateVerdict(v, c))
}

func TestValidateVerdict_RejectsNil(t *testing.T) {
	c := healthyConfig()
	require.ErrorContains(t, ValidateVerdict(nil, c), "nil verdict")
}

func TestValidateVerdict_RejectsUnknownOperator(t *testing.T) {
	c := healthyConfig()
	v := validVerdict(c)
	v.OperatorID = 99 // not in cluster
	require.ErrorContains(t, ValidateVerdict(v, c), "not in cluster")
}

func TestValidateVerdict_RejectsLayerOutOfRange(t *testing.T) {
	c := healthyConfig()
	v := validVerdict(c)
	v.Layer = c.K()
	require.ErrorContains(t, ValidateVerdict(v, c), "out of range")
}

func TestValidateVerdict_RejectsClusterMismatch(t *testing.T) {
	c := healthyConfig()
	v := validVerdict(c)
	v.ClusterID = [32]byte{0xff}
	require.ErrorContains(t, ValidateVerdict(v, c), "cluster id")
}

func TestValidateVerdict_RejectsSigmaVWithZeroValueRoot(t *testing.T) {
	c := healthyConfig()
	v := validVerdict(c)
	v.ValueRoot = [32]byte{} // σV but null root → invalid
	require.ErrorContains(t, ValidateVerdict(v, c), "zero ValueRoot")
}

func TestValidateVerdict_RejectsNRWithNonZeroValueRoot(t *testing.T) {
	c := healthyConfig()
	v := validVerdict(c)
	v.Kind = VerdictNR
	v.ValueRoot = [32]byte{0x01} // NR must have null root
	require.ErrorContains(t, ValidateVerdict(v, c), "zero ValueRoot")
}

func TestValidateVerdict_RejectsUnspecified(t *testing.T) {
	c := healthyConfig()
	v := validVerdict(c)
	v.Kind = VerdictUnspecified
	require.ErrorContains(t, ValidateVerdict(v, c), "unspecified")
}

// ---------- ValidateOnion2b ----------

func validOnion2b(c *Config) *Onion2b {
	layers := make([]EncryptedLayer, c.K())
	// σ-emit at layer 0 plaintext; NR at layers 1, 2; nothing at K-1
	// (deepest — no NR tag).
	layers[0] = EncryptedLayer{Value: Value("V0"), Ciphertext: []byte("σ0")}
	return &Onion2b{
		ClusterID:  c.ClusterID,
		OperatorID: c.Operators[0],
		Height:     c.Height,
		Layers:     layers,
		NRPartials: []NRPartial{
			{Layer: 1, PartialSig: Signature("nr1")},
			{Layer: 2, PartialSig: Signature("nr2")},
		},
	}
}

func TestValidateOnion2b_AcceptsHealthy(t *testing.T) {
	c := healthyConfig()
	require.NoError(t, ValidateOnion2b(validOnion2b(c), c))
}

func TestValidateOnion2b_RejectsNil(t *testing.T) {
	c := healthyConfig()
	require.ErrorContains(t, ValidateOnion2b(nil, c), "nil onion2b")
}

func TestValidateOnion2b_RejectsWrongLayerCount(t *testing.T) {
	c := healthyConfig()
	o := validOnion2b(c)
	o.Layers = o.Layers[:c.K()-1] // wrong count
	require.ErrorContains(t, ValidateOnion2b(o, c), "expected K=")
}

func TestValidateOnion2b_RejectsUnknownOperator(t *testing.T) {
	c := healthyConfig()
	o := validOnion2b(c)
	o.OperatorID = 99
	require.ErrorContains(t, ValidateOnion2b(o, c), "not in cluster")
}

func TestValidateOnion2b_RejectsHalfPopulatedLayer(t *testing.T) {
	c := healthyConfig()
	o := validOnion2b(c)
	// Value present but no Ciphertext → half-populated
	o.Layers[2] = EncryptedLayer{Value: Value("V2"), Ciphertext: nil}
	require.ErrorContains(t, ValidateOnion2b(o, c), "half-populated")
}

func TestValidateOnion2b_RejectsNRAtDeepestLayer(t *testing.T) {
	c := healthyConfig()
	o := validOnion2b(c)
	o.NRPartials = append(o.NRPartials, NRPartial{
		Layer:      c.K() - 1, // deepest layer has no NR tag
		PartialSig: Signature("nr_deepest"),
	})
	require.ErrorContains(t, ValidateOnion2b(o, c), "out of valid range")
}

func TestValidateOnion2b_RejectsDuplicateNRLayer(t *testing.T) {
	c := healthyConfig()
	o := validOnion2b(c)
	o.NRPartials = append(o.NRPartials, NRPartial{
		Layer: 1, PartialSig: Signature("nr1-dup"),
	})
	require.ErrorContains(t, ValidateOnion2b(o, c), "duplicate NR layer")
}

func TestValidateOnion2b_RejectsEmptyNRPartialSig(t *testing.T) {
	c := healthyConfig()
	o := validOnion2b(c)
	o.NRPartials[0].PartialSig = nil
	require.ErrorContains(t, ValidateOnion2b(o, c), "empty signature")
}

// ---------- ValidateCertificate ----------

func TestValidateCertificate_AcceptsHealthy(t *testing.T) {
	c := healthyConfig()
	cert := &Certificate{
		ClusterID: c.ClusterID,
		Height:    c.Height,
		Value:     Value("V"),
		Signature: Signature("agg-sig"),
	}
	require.NoError(t, ValidateCertificate(cert, c))
}

func TestValidateCertificate_RejectsEmptyValue(t *testing.T) {
	c := healthyConfig()
	cert := &Certificate{
		ClusterID: c.ClusterID,
		Height:    c.Height,
		Signature: Signature("agg-sig"),
	}
	require.ErrorContains(t, ValidateCertificate(cert, c), "empty Value")
}

func TestValidateCertificate_RejectsEmptySignature(t *testing.T) {
	c := healthyConfig()
	cert := &Certificate{
		ClusterID: c.ClusterID,
		Height:    c.Height,
		Value:     Value("V"),
	}
	require.ErrorContains(t, ValidateCertificate(cert, c), "empty Signature")
}

func TestValidateCertificate_RejectsClusterMismatch(t *testing.T) {
	c := healthyConfig()
	cert := &Certificate{
		ClusterID: [32]byte{0xff},
		Height:    c.Height,
		Value:     Value("V"),
		Signature: Signature("sig"),
	}
	require.ErrorContains(t, ValidateCertificate(cert, c), "cluster id")
}
