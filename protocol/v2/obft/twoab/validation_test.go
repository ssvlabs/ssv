package twoab

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// stubWitness is a non-empty placeholder for L0Witness in validation
// tests. ValidatePhase1Bundle does NOT BLS-verify the witness — only
// checks non-emptiness at L_0. BLS verification happens at
// ObservePhase1Bundle (instance layer); tests for that path provide
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
		L0Witness:  stubWitness,
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
	b := &Phase1Bundle{ClusterID: [32]byte{0xff}, Height: cfg.Height, Layer: 0, OperatorID: cfg.Layers[0].Leader, Value: Value("V0"), L0Witness: stubWitness}
	require.Error(t, ValidatePhase1Bundle(b, cfg))
}

func TestValidatePhase1Bundle_RejectsHeightMismatch(t *testing.T) {
	cfg := healthyConfig()
	b := &Phase1Bundle{ClusterID: cfg.ClusterID, Height: 999, Layer: 0, OperatorID: cfg.Layers[0].Leader, Value: Value("V0"), L0Witness: stubWitness}
	require.Error(t, ValidatePhase1Bundle(b, cfg))
}

func TestValidatePhase1Bundle_RejectsLayerOutOfRange(t *testing.T) {
	cfg := healthyConfig()
	b := &Phase1Bundle{ClusterID: cfg.ClusterID, Height: cfg.Height, Layer: 99, OperatorID: cfg.Layers[0].Leader, Value: Value("V0"), L0Witness: stubWitness}
	require.Error(t, ValidatePhase1Bundle(b, cfg))
}

func TestValidatePhase1Bundle_RejectsWrongLeader(t *testing.T) {
	cfg := healthyConfig()
	b := &Phase1Bundle{ClusterID: cfg.ClusterID, Height: cfg.Height, Layer: 0, OperatorID: 99, Value: Value("V0"), L0Witness: stubWitness}
	require.Error(t, ValidatePhase1Bundle(b, cfg))
}

func TestValidatePhase1Bundle_RejectsEmptyValue(t *testing.T) {
	cfg := healthyConfig()
	b := &Phase1Bundle{ClusterID: cfg.ClusterID, Height: cfg.Height, Layer: 0, OperatorID: cfg.Layers[0].Leader, Value: Value{}, L0Witness: stubWitness}
	require.Error(t, ValidatePhase1Bundle(b, cfg))
}

func TestValidatePhase1Bundle_RejectsEmptyL0WitnessAtL0(t *testing.T) {
	cfg := healthyConfig()
	b := &Phase1Bundle{ClusterID: cfg.ClusterID, Height: cfg.Height, Layer: 0, OperatorID: cfg.Layers[0].Leader, Value: Value("V0")}
	require.Error(t, ValidatePhase1Bundle(b, cfg))
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
		L0Witness:    Signature{0x01}, // structural: any non-empty bytes pass validation
		LayerEntries: []LayerEntry{{Layer: 1, Kind: LayerEntryEmpty}},
	}
	require.NoError(t, ValidateValueMsg(vm, cfg))
}

func TestValidateValueMsg_RejectsEmptyL0Witness(t *testing.T) {
	cfg := healthyConfig()
	v := Value("V0")
	vm := &ValueMsg{
		ClusterID:    cfg.ClusterID,
		OperatorID:   1,
		Height:       cfg.Height,
		V:            v,
		ValueRoot:    ValueRoot(v),
		LayerEntries: []LayerEntry{{Layer: 1, Kind: LayerEntryEmpty}},
	}
	require.Error(t, ValidateValueMsg(vm, cfg),
		"Op11: ValueMsg with empty L0Witness should be rejected")
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
	// K=2 expects K-1=1 entry; provide 0.
	vm := &ValueMsg{ClusterID: cfg.ClusterID, OperatorID: 1, Height: cfg.Height, V: v, ValueRoot: ValueRoot(v)}
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

func TestValidateCommit_AcceptsSigned(t *testing.T) {
	cfg := healthyConfig()
	c := &Commit{
		ClusterID:  cfg.ClusterID,
		OperatorID: 1,
		Height:     cfg.Height,
		Side:       CommitSideSigned,
		L0Value:    Value("V0"),
		L0Partial:  Signature("partial"),
	}
	require.NoError(t, ValidateCommit(c, cfg))
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

func TestValidateCommit_RejectsSignedWithoutL0Value(t *testing.T) {
	cfg := healthyConfig()
	c := &Commit{ClusterID: cfg.ClusterID, OperatorID: 1, Height: cfg.Height, Side: CommitSideSigned, L0Partial: Signature("p")}
	require.Error(t, ValidateCommit(c, cfg))
}

func TestValidateCommit_RejectsNRWithL0Value(t *testing.T) {
	cfg := healthyConfig()
	c := &Commit{ClusterID: cfg.ClusterID, OperatorID: 1, Height: cfg.Height, Side: CommitSideNR, L0Value: Value("V0"), L0Partial: Signature("p")}
	require.Error(t, ValidateCommit(c, cfg))
}

func TestValidateCommit_RejectsSignedWithLayerEntries(t *testing.T) {
	cfg := healthyConfig()
	c := &Commit{
		ClusterID:    cfg.ClusterID,
		OperatorID:   1,
		Height:       cfg.Height,
		Side:         CommitSideSigned,
		L0Value:      Value("V0"),
		L0Partial:    Signature("p"),
		LayerEntries: []LayerEntry{{Layer: 1, Kind: LayerEntryEmpty}},
	}
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
