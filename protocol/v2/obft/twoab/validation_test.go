package twoab

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// stubWitness is a non-empty placeholder for LWitness in validation
// tests. ValidatePhase1Bundle does NOT BLS-verify the witness — only
// checks non-emptiness (at every layer post-Op8). BLS verification happens
// at ObservePhase1Bundle (instance layer); tests for that path provide
// genuinely signed witnesses.
var stubWitness = Signature{0xaa, 0xbb, 0xcc}

func TestValidatePhase1Bundle_AcceptsHealthy(t *testing.T) {
	cfg := healthyConfig()
	b := &Phase1Bundle{
		ClusterID:  cfg.ClusterID,
		OperatorID: cfg.Layers[0].Leader,
		Height:     cfg.Height,
		Layer:      0,
		Value:      Value("V0"),
		LWitness:   stubWitness,
	}
	require.NoError(t, ValidatePhase1Bundle(b, cfg))
}

func TestValidatePhase1Bundle_RejectsNil(t *testing.T) {
	cfg := healthyConfig()
	require.Error(t, ValidatePhase1Bundle(nil, cfg))
}

func TestValidatePhase1Bundle_RejectsNilConfig(t *testing.T) {
	b := &Phase1Bundle{}
	require.Error(t, ValidatePhase1Bundle(b, nil))
}

func TestValidatePhase1Bundle_RejectsClusterMismatch(t *testing.T) {
	cfg := healthyConfig()
	b := &Phase1Bundle{ClusterID: [32]byte{0xff}, Height: cfg.Height, Layer: 0, OperatorID: cfg.Layers[0].Leader, Value: Value("V0"), LWitness: stubWitness}
	require.Error(t, ValidatePhase1Bundle(b, cfg))
}

func TestValidatePhase1Bundle_RejectsHeightMismatch(t *testing.T) {
	cfg := healthyConfig()
	b := &Phase1Bundle{ClusterID: cfg.ClusterID, Height: 999, Layer: 0, OperatorID: cfg.Layers[0].Leader, Value: Value("V0"), LWitness: stubWitness}
	require.Error(t, ValidatePhase1Bundle(b, cfg))
}

func TestValidatePhase1Bundle_RejectsLayerOutOfRange(t *testing.T) {
	cfg := healthyConfig()
	b := &Phase1Bundle{ClusterID: cfg.ClusterID, Height: cfg.Height, Layer: 99, OperatorID: cfg.Layers[0].Leader, Value: Value("V0"), LWitness: stubWitness}
	require.Error(t, ValidatePhase1Bundle(b, cfg))
}

func TestValidatePhase1Bundle_RejectsWrongLeader(t *testing.T) {
	cfg := healthyConfig()
	b := &Phase1Bundle{ClusterID: cfg.ClusterID, Height: cfg.Height, Layer: 0, OperatorID: 99, Value: Value("V0"), LWitness: stubWitness}
	require.Error(t, ValidatePhase1Bundle(b, cfg))
}

func TestValidatePhase1Bundle_RejectsEmptyValue(t *testing.T) {
	cfg := healthyConfig()
	b := &Phase1Bundle{ClusterID: cfg.ClusterID, Height: cfg.Height, Layer: 0, OperatorID: cfg.Layers[0].Leader, Value: Value{}, LWitness: stubWitness}
	require.Error(t, ValidatePhase1Bundle(b, cfg))
}

// TestValidatePhase1Bundle_RejectsEmptyLWitness verifies that post-Op8 an
// empty LWitness is rejected at EVERY layer (was L_0-only under Op3).
func TestValidatePhase1Bundle_RejectsEmptyLWitness(t *testing.T) {
	cfg := healthyConfig() // K=2: layer 0 (leader op1), layer 1 (leader op2).
	b0 := &Phase1Bundle{ClusterID: cfg.ClusterID, Height: cfg.Height, Layer: 0, OperatorID: cfg.Layers[0].Leader, Value: Value("V0")}
	require.Error(t, ValidatePhase1Bundle(b0, cfg), "empty LWitness at L_0 must be rejected")
	b1 := &Phase1Bundle{ClusterID: cfg.ClusterID, Height: cfg.Height, Layer: 1, OperatorID: cfg.Layers[1].Leader, Value: Value("V1")}
	require.Error(t, ValidatePhase1Bundle(b1, cfg), "Op8: empty LWitness at L_k>0 must be rejected")
}

func TestValidateValueMsg_AcceptsHealthy(t *testing.T) {
	cfg := healthyConfig()
	v := Value("V0")
	vm := &ValueMsg{
		ClusterID:    cfg.ClusterID,
		OperatorID:   1,
		Height:       cfg.Height,
		V:            v,
		ValueRoot:    ValueRoot(v),
		Witnesses:    l0Witness(v, Signature{0x01}), // structural: any non-empty bytes pass validation
		L0Partial:    Signature{0x01},               // Op5: arbitrary; verify fails (Rule 5 OK for these tests focused on Rule 6a)
		LayerEntries: []LayerEntry{{Layer: 1, Kind: LayerEntryEmpty}},
	}
	require.NoError(t, ValidateValueMsg(vm, cfg))
}

// TestValidateValueMsg_RejectsMissingOrBadWitnesses covers the Op12
// Witnesses[] structural checks (replacing the former single-L0Witness
// check). cfg.K()==2, so Layer 1 is in range.
func TestValidateValueMsg_RejectsMissingOrBadWitnesses(t *testing.T) {
	cfg := healthyConfig()
	v := Value("V0")
	base := func() *ValueMsg {
		return &ValueMsg{
			ClusterID: cfg.ClusterID, OperatorID: 1, Height: cfg.Height,
			V: v, ValueRoot: ValueRoot(v),
			L0Partial:    Signature{0x01},
			LayerEntries: []LayerEntry{{Layer: 1, Kind: LayerEntryEmpty}},
		}
	}
	// No witnesses at all → reject (a KindValue must forward the L_0 witness).
	require.Error(t, ValidateValueMsg(base(), cfg),
		"Op11/Op12: ValueMsg with no Witnesses should be rejected")
	// Witnesses present but missing the mandatory Layer-0 entry → reject.
	vm := base()
	vm.Witnesses = []LayerWitness{{Layer: 1, ValueRoot: ValueRoot(Value("V1")), Witness: Signature{0x01}}}
	require.Error(t, ValidateValueMsg(vm, cfg),
		"Op12: ValueMsg without a Layer-0 witness should be rejected")
	// Layer-0 witness whose ValueRoot != v.ValueRoot → reject.
	vm = base()
	vm.Witnesses = []LayerWitness{{Layer: 0, ValueRoot: ValueRoot(Value("other")), Witness: Signature{0x01}}}
	require.Error(t, ValidateValueMsg(vm, cfg),
		"Op12: Layer-0 witness ValueRoot must match v.ValueRoot")
	// Duplicate layer → reject.
	vm = base()
	vm.Witnesses = []LayerWitness{
		{Layer: 0, ValueRoot: ValueRoot(v), Witness: Signature{0x01}},
		{Layer: 0, ValueRoot: ValueRoot(v), Witness: Signature{0x02}},
	}
	require.Error(t, ValidateValueMsg(vm, cfg),
		"Op12: duplicate witness layer should be rejected")
	// Empty witness bytes → reject.
	vm = base()
	vm.Witnesses = []LayerWitness{{Layer: 0, ValueRoot: ValueRoot(v), Witness: nil}}
	require.Error(t, ValidateValueMsg(vm, cfg),
		"Op12: empty witness signature should be rejected")
}

func TestValidateValueMsg_RejectsEmptyL0Partial(t *testing.T) {
	cfg := healthyConfig()
	v := Value("V0")
	vm := &ValueMsg{
		ClusterID:  cfg.ClusterID,
		OperatorID: 1,
		Height:     cfg.Height,
		V:          v,
		ValueRoot:  ValueRoot(v),
		Witnesses:  l0Witness(v, Signature{0x01}),
		// L0Partial intentionally empty — Op5 requires the emitter's own σ partial.
		LayerEntries: []LayerEntry{{Layer: 1, Kind: LayerEntryEmpty}},
	}
	require.Error(t, ValidateValueMsg(vm, cfg),
		"Op5: ValueMsg with empty L0Partial should be rejected")
}

func TestValidateValueMsg_RejectsEmptyV(t *testing.T) {
	cfg := healthyConfig()
	vm := &ValueMsg{ClusterID: cfg.ClusterID, OperatorID: 1, Height: cfg.Height, V: Value{}, LayerEntries: []LayerEntry{{Layer: 1, Kind: LayerEntryEmpty}}}
	require.Error(t, ValidateValueMsg(vm, cfg))
}

func TestValidateValueMsg_RejectsValueRootMismatch(t *testing.T) {
	cfg := healthyConfig()
	vm := &ValueMsg{
		ClusterID:    cfg.ClusterID,
		OperatorID:   1,
		Height:       cfg.Height,
		V:            Value("V0"),
		ValueRoot:    [32]byte{0xff},
		LayerEntries: []LayerEntry{{Layer: 1, Kind: LayerEntryEmpty}},
	}
	require.Error(t, ValidateValueMsg(vm, cfg))
}

func TestValidateValueMsg_RejectsUnknownOperator(t *testing.T) {
	cfg := healthyConfig()
	v := Value("V0")
	vm := &ValueMsg{ClusterID: cfg.ClusterID, OperatorID: 99, Height: cfg.Height, V: v, ValueRoot: ValueRoot(v), LayerEntries: []LayerEntry{{Layer: 1, Kind: LayerEntryEmpty}}}
	require.Error(t, ValidateValueMsg(vm, cfg))
}

func TestValidateValueMsg_RejectsWrongLayerEntryCount(t *testing.T) {
	cfg := healthyConfig()
	v := Value("V0")
	// K=2 expects K-1=1 entry; provide 0. Supply valid Witnesses + L0Partial
	// so validation reaches the layer-count check rather than short-circuiting
	// on the earlier Witnesses/L0Partial checks.
	vm := &ValueMsg{
		ClusterID: cfg.ClusterID, OperatorID: 1, Height: cfg.Height,
		V: v, ValueRoot: ValueRoot(v),
		Witnesses: l0Witness(v, Signature{0x01}),
		L0Partial: Signature{0x01},
	}
	require.Error(t, ValidateValueMsg(vm, cfg))
}

func TestValidateNoValueMsg_AcceptsHealthy(t *testing.T) {
	cfg := healthyConfig()
	nv := &NoValueMsg{
		ClusterID:    cfg.ClusterID,
		OperatorID:   1,
		Height:       cfg.Height,
		LayerEntries: []LayerEntry{{Layer: 1, Kind: LayerEntryEmpty}},
	}
	require.NoError(t, ValidateNoValueMsg(nv, cfg))
}

func TestValidateNoValueMsg_RejectsNil(t *testing.T) {
	cfg := healthyConfig()
	require.Error(t, ValidateNoValueMsg(nil, cfg))
}

// TestValidateCommit_RejectsSignedSidePostOp5: the pre-Op5 Side=Signed
// value (0x01) is no longer valid. Decoders/validators reject it loudly
// to surface cluster-wide wire-version drift.
func TestValidateCommit_RejectsSignedSidePostOp5(t *testing.T) {
	cfg := healthyConfig()
	c := &Commit{
		ClusterID:  cfg.ClusterID,
		OperatorID: 1,
		Height:     cfg.Height,
		Side:       CommitSide(0x01), // pre-Op5 Signed value
		L0Partial:  Signature("partial"),
	}
	require.Error(t, ValidateCommit(c, cfg),
		"Op5: pre-Op5 CommitSideSigned (0x01) must be rejected")
}

func TestValidateCommit_AcceptsNR(t *testing.T) {
	cfg := healthyConfig()
	c := &Commit{ClusterID: cfg.ClusterID, OperatorID: 1, Height: cfg.Height, Side: CommitSideNR, L0Partial: Signature("partial")}
	require.NoError(t, ValidateCommit(c, cfg))
}

func TestValidateCommit_AcceptsNRDirect(t *testing.T) {
	cfg := healthyConfig()
	c := &Commit{
		ClusterID:    cfg.ClusterID,
		OperatorID:   1,
		Height:       cfg.Height,
		Side:         CommitSideNRDirect,
		L0Partial:    Signature("partial"),
		LayerEntries: []LayerEntry{{Layer: 1, Kind: LayerEntryEmpty}},
	}
	require.NoError(t, ValidateCommit(c, cfg))
}

func TestValidateCommit_RejectsUnspecifiedSide(t *testing.T) {
	cfg := healthyConfig()
	c := &Commit{ClusterID: cfg.ClusterID, OperatorID: 1, Height: cfg.Height, Side: CommitSideUnspecified, L0Partial: Signature("p")}
	require.Error(t, ValidateCommit(c, cfg))
}

func TestValidateCertificate_AcceptsHealthy(t *testing.T) {
	cfg := healthyConfig()
	c := &Certificate{ClusterID: cfg.ClusterID, Height: cfg.Height, Value: Value("V0"), Signature: Signature("sig")}
	require.NoError(t, ValidateCertificate(c, cfg))
}

func TestValidateCertificate_RejectsEmptyValue(t *testing.T) {
	cfg := healthyConfig()
	c := &Certificate{ClusterID: cfg.ClusterID, Height: cfg.Height, Signature: Signature("sig")}
	require.Error(t, ValidateCertificate(c, cfg))
}

func TestValidateLayerEntries_RejectsWrongCount(t *testing.T) {
	cfg := healthyConfig() // K=2 → expects 1 entry
	v := Value("V0")
	vm := &ValueMsg{
		ClusterID:    cfg.ClusterID,
		OperatorID:   1,
		Height:       cfg.Height,
		V:            v,
		ValueRoot:    ValueRoot(v),
		LayerEntries: []LayerEntry{{Layer: 1, Kind: LayerEntryEmpty}, {Layer: 2, Kind: LayerEntryEmpty}}, // 2 entries, wrong
	}
	require.Error(t, ValidateValueMsg(vm, cfg))
}

func TestValidateLayerEntries_RejectsDuplicateLayer(t *testing.T) {
	cfg := healthyConfig()
	// To produce a duplicate, we need K>=3 so the array has 2 entries (K-1=2).
	cfgK3 := *cfg
	extraLayer := LayerSpec{Leader: 3, FetchAt: 0, BroadcastBudget: cfg.Layers[1].BroadcastBudget}
	cfgK3.Layers = append([]LayerSpec{}, cfg.Layers...)
	cfgK3.Layers = append(cfgK3.Layers, extraLayer)
	v := Value("V0")
	vm := &ValueMsg{
		ClusterID:    cfgK3.ClusterID,
		OperatorID:   1,
		Height:       cfgK3.Height,
		V:            v,
		ValueRoot:    ValueRoot(v),
		LayerEntries: []LayerEntry{{Layer: 1, Kind: LayerEntryEmpty}, {Layer: 1, Kind: LayerEntryEmpty}}, // duplicate
	}
	require.Error(t, ValidateValueMsg(vm, &cfgK3))
}

func TestValidateLayerEntries_RejectsNRPlaintextAtDeepestLayer(t *testing.T) {
	cfg := healthyConfig() // K=2 → deepest layer is 1
	v := Value("V0")
	vm := &ValueMsg{
		ClusterID:    cfg.ClusterID,
		OperatorID:   1,
		Height:       cfg.Height,
		V:            v,
		ValueRoot:    ValueRoot(v),
		LayerEntries: []LayerEntry{{Layer: 1, Kind: LayerEntryNRPlaintext, Payload: []byte("p")}},
	}
	require.Error(t, ValidateValueMsg(vm, cfg))
}
