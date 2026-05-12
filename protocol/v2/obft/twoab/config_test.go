package twoab

import (
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// healthyConfig returns a spec-conforming Config at the default K=4, f=1,
// n=4 deployment (Config A: BTT=200ms, Δ_2a=Δ_2b=400ms, Δ_3=250ms,
// TCommit=2000ms). Tests mutate fields to check Validate rejects the
// invalid input.
func healthyConfig() *Config {
	btt := 200 * time.Millisecond
	tCommit := 2000 * time.Millisecond
	tVerdictStart := tCommit - 400*time.Millisecond // = 1600ms (= 2 BTT before TCommit)
	budgets, err := DefaultBroadcastBudget(4, btt, tVerdictStart)
	if err != nil {
		panic(err)
	}
	// FetchAt strictly-decreasing in layer index; deepest at slot start.
	fetchAt := []time.Duration{
		tVerdictStart - budgets[0], // L_0 fetch deadline
		tVerdictStart - budgets[1],
		tVerdictStart - budgets[2],
		0, // L_3 (deepest) fetches at slot_start
	}
	return &Config{
		Height:    Height(42),
		ClusterID: [32]byte{0xab},
		Operators: []OperatorID{1, 2, 3, 4},
		F:         1,
		Layers: []LayerSpec{
			{Leader: 1, FetchAt: fetchAt[0], BroadcastBudget: budgets[0]},
			{Leader: 2, FetchAt: fetchAt[1], BroadcastBudget: budgets[1]},
			{Leader: 3, FetchAt: fetchAt[2], BroadcastBudget: budgets[2]},
			{Leader: 4, FetchAt: fetchAt[3], BroadcastBudget: budgets[3]},
		},
		TCommit: tCommit,
		Delta2a: 400 * time.Millisecond,
		Delta2b: 400 * time.Millisecond,
		Delta3:  250 * time.Millisecond,
		BTT:     btt,
	}
}

func TestConfig_Validate_HealthyConfigAccepted(t *testing.T) {
	c := healthyConfig()
	require.NoError(t, c.Validate())
}

func TestConfig_Validate_RejectsZeroF(t *testing.T) {
	c := healthyConfig()
	c.F = 0
	require.ErrorContains(t, c.Validate(), "byzantine bound F")
}

func TestConfig_Validate_RejectsClusterBelow3Fplus1(t *testing.T) {
	c := healthyConfig()
	c.Operators = []OperatorID{1, 2, 3} // F=1, need 4
	require.ErrorContains(t, c.Validate(), "at least 3F+1")
}

func TestConfig_Validate_RejectsKBelowBFTLivenessMinimum(t *testing.T) {
	// At f=1 the BFT-liveness minimum is K=f+1=2; K=1 has no honest-leader
	// guarantee and is rejected.
	c := healthyConfig()
	c.Layers = c.Layers[:1]
	require.ErrorContains(t, c.Validate(), "below BFT-liveness minimum")
}

// K=2 at F=1 is the BFT-liveness minimum and is supported. Spec §Setting
// leaves the choice between K=f+1 (smaller envelope) and K≥f+2 (late-leader
// resilient) to the operator. Builds a fresh K=2 config from scratch — the
// healthyConfig() shallow B_k values are sized for K=4 and don't satisfy
// the deepest-layer BFT-min (2·BTT) once shortened to K=2.
func TestConfig_Validate_AllowsKAtBFTLivenessMinimum(t *testing.T) {
	btt := 200 * time.Millisecond
	tCommit := 2000 * time.Millisecond
	tVerdictStart := tCommit - 400*time.Millisecond // = 1600ms
	c := &Config{
		Height:    Height(42),
		ClusterID: [32]byte{0xab},
		Operators: []OperatorID{1, 2, 3, 4},
		F:         1,
		Layers: []LayerSpec{
			{Leader: 1, FetchAt: tVerdictStart - btt, BroadcastBudget: btt},
			{Leader: 2, FetchAt: 0, BroadcastBudget: tVerdictStart},
		},
		TCommit: tCommit,
		Delta2a: 400 * time.Millisecond,
		Delta2b: 400 * time.Millisecond,
		Delta3:  250 * time.Millisecond,
		BTT:     btt,
	}
	require.NoError(t, c.Validate())
	require.Equal(t, 2, c.K())
}

func TestConfig_Validate_RejectsKAboveClusterSize(t *testing.T) {
	c := healthyConfig()
	// Add a phantom layer beyond the n=4 operator set.
	c.Layers = append(c.Layers, LayerSpec{
		Leader:          5, // not a member
		FetchAt:         0,
		BroadcastBudget: c.Layers[3].BroadcastBudget + time.Millisecond,
	})
	// K=5 > n=4 should fail before the membership check.
	require.ErrorContains(t, c.Validate(), "K cannot exceed cluster size")
}

func TestConfig_Validate_RejectsNonPositiveBTT(t *testing.T) {
	c := healthyConfig()
	c.BTT = 0
	require.ErrorContains(t, c.Validate(), "BTT must be positive")
}

func TestConfig_Validate_RejectsNonPositiveTCommit(t *testing.T) {
	c := healthyConfig()
	c.TCommit = 0
	require.ErrorContains(t, c.Validate(), "TCommit must be positive")
}

// Δ_2a = 1 BTT is broken-by-construction per spec §Setting — Validate
// must reject it explicitly, not just "below minimum".
func TestConfig_Validate_RejectsDelta2aBelowTwoBTT(t *testing.T) {
	c := healthyConfig()
	c.Delta2a = c.BTT // = 1 BTT, broken-by-construction
	require.ErrorContains(t, c.Validate(), "Delta2a must be >= 2 BTT")
}

func TestConfig_Validate_AcceptsDelta2aAtTwoBTT(t *testing.T) {
	c := healthyConfig()
	c.Delta2a = 2 * c.BTT // = 2 BTT, minimum coherent sizing
	require.NoError(t, c.Validate())
}

func TestConfig_Validate_RejectsDelta2bBelowOneBTT(t *testing.T) {
	c := healthyConfig()
	c.Delta2b = c.BTT / 2 // < 1 BTT
	require.ErrorContains(t, c.Validate(), "Delta2b must be >= 1 BTT")
}

func TestConfig_Validate_RejectsNonPositiveDelta3(t *testing.T) {
	c := healthyConfig()
	c.Delta3 = 0
	require.ErrorContains(t, c.Validate(), "Delta3 must be positive")
}

func TestConfig_Validate_RejectsTCommitBelowDelta2a(t *testing.T) {
	c := healthyConfig()
	// TCommit ≤ Delta2a would make TVerdictStart ≤ 0 (impossible).
	c.TCommit = c.Delta2a
	require.ErrorContains(t, c.Validate(), "TCommit must be > Delta2a")
}

func TestConfig_Validate_RejectsDuplicateOperator(t *testing.T) {
	c := healthyConfig()
	c.Operators = []OperatorID{1, 2, 3, 3} // duplicate
	require.ErrorContains(t, c.Validate(), "duplicate operator")
}

func TestConfig_Validate_RejectsLayerWithZeroBroadcastBudget(t *testing.T) {
	c := healthyConfig()
	c.Layers[2].BroadcastBudget = 0
	err := c.Validate()
	require.Error(t, err)
	require.True(t, strings.Contains(err.Error(), "BroadcastBudget must be > 0"),
		"got: %v", err)
}

func TestConfig_Validate_RejectsDecreasingBroadcastBudgets(t *testing.T) {
	c := healthyConfig()
	// Swap L_1 and L_2 budgets so the schedule strictly decreases at L_2.
	// Non-decreasing is the relaxed invariant (ties allowed when shallow
	// targets clamp to BFT_start); an actual decrease is rejected.
	c.Layers[1].BroadcastBudget, c.Layers[2].BroadcastBudget =
		c.Layers[2].BroadcastBudget, c.Layers[1].BroadcastBudget
	require.ErrorContains(t, c.Validate(), "non-decreasing")
}

// Equal adjacent BroadcastBudget entries are accepted post-relaxation —
// at degraded operating points the canonical schedule pushes multiple
// shallow layers' targets to clamp at BFT_start, which materializes as
// ties in B_k across adjacent layers.
func TestConfig_Validate_AcceptsEqualAdjacentBroadcastBudgets(t *testing.T) {
	c := healthyConfig()
	c.Layers[1].BroadcastBudget = c.Layers[0].BroadcastBudget
	c.Layers[1].FetchAt = c.Layers[0].FetchAt // collide at the same BFT_start anchor
	require.NoError(t, c.Validate())
}

func TestConfig_Validate_RejectsDeepestBudgetBelowBFTMin(t *testing.T) {
	c := healthyConfig()
	// Manually craft a Config where the deepest budget is below 2*BTT.
	// Default schedule at K=4 BTT=200ms gives B_3 = TVerdictStart = 1600ms,
	// well above 2*BTT=400ms. Force it down.
	c.Layers[3].BroadcastBudget = c.BTT // = 1 BTT, < 2*BTT
	// Need to also adjust earlier budgets so strict-increasing still holds.
	c.Layers[0].BroadcastBudget = c.BTT / 4
	c.Layers[1].BroadcastBudget = c.BTT / 3
	c.Layers[2].BroadcastBudget = c.BTT / 2
	// FetchAt also needs to be within new deadlines.
	for k := range c.Layers {
		c.Layers[k].FetchAt = 0 // safest: fetch at slot start
	}
	require.ErrorContains(t, c.Validate(), "below BFT-min")
}

func TestConfig_Validate_RejectsLayerLeaderNotInCluster(t *testing.T) {
	c := healthyConfig()
	c.Layers[1].Leader = 99 // not in Operators
	require.ErrorContains(t, c.Validate(), "leader is not a cluster member")
}

func TestConfig_Validate_RejectsDuplicateLayerLeader(t *testing.T) {
	c := healthyConfig()
	c.Layers[2].Leader = c.Layers[0].Leader // duplicate
	require.ErrorContains(t, c.Validate(), "duplicate leader")
}

func TestConfig_Validate_RejectsIncreasingFetchAt(t *testing.T) {
	c := healthyConfig()
	// Pure-increase in k (L_1 > L_0) violates non-increasing. Equal
	// adjacent FetchAt entries are accepted (collide-at-BFT_start case);
	// only a strict increase in k is rejected.
	c.Layers[0].FetchAt = 1000 * time.Millisecond
	c.Layers[1].FetchAt = 1100 * time.Millisecond
	require.ErrorContains(t, c.Validate(), "non-increasing")
}

func TestConfig_Validate_AcceptsEqualAdjacentFetchAt(t *testing.T) {
	c := healthyConfig()
	// Tied FetchAt at adjacent deep layers — what materializes when both
	// layers' broadcast targets clamp at BFT_start. Tie L_2 and L_3 at 0
	// (the natural collide-at-BFT_start position for the deepest pair);
	// shallower layers stay at their original strictly-decreasing values
	// so the wider non-increasing invariant still holds.
	c.Layers[2].FetchAt = 0
	c.Layers[3].FetchAt = 0
	require.NoError(t, c.Validate())
}

func TestConfig_Validate_RejectsFetchAtBeyondBroadcastDeadline(t *testing.T) {
	c := healthyConfig()
	// Push L_0 FetchAt past its broadcast deadline.
	c.Layers[0].FetchAt = c.BroadcastMaxOffsetForLayer(0) + time.Millisecond
	require.ErrorContains(t, c.Validate(), "exceeds broadcast deadline")
}

// --- Derived offsets ---

func TestConfig_DerivedOffsets_HealthyConfig(t *testing.T) {
	c := healthyConfig()
	require.Equal(t, 1600*time.Millisecond, c.TVerdictStart(),
		"TVerdictStart = TCommit − Δ_2a = 2000 − 400")
	require.Equal(t, 1800*time.Millisecond, c.TAcceptMax(),
		"TAcceptMax = TCommit − 1 BTT = 2000 − 200")
	require.Equal(t, c.TAcceptMax(), c.TVerdictMax(),
		"TVerdictMax coincides with TAcceptMax by construction")
	require.Equal(t, c.TVerdictStart(), c.Phase2aStartOffset())
	require.Equal(t, c.TCommit, c.Phase2aEndOffset())
	require.Equal(t, c.TCommit, c.Phase2bStartOffset())
	require.Equal(t, c.TCommit+c.Delta2b, c.Phase2bEndOffset())
	require.Equal(t, c.TCommit+c.Delta2b, c.Phase3StartOffset())
	require.Equal(t, c.TCommit+c.Delta2b+c.Delta3, c.RoundEndOffset())
}

func TestConfig_QV_QEnc_Quorum(t *testing.T) {
	c := healthyConfig()
	require.Equal(t, 3, c.QV(), "qV = 2f+1 = 3 at f=1")
	require.Equal(t, 3, c.QEnc(), "qEnc = 2f+1 = 3 at f=1")
	require.Equal(t, c.QV(), c.Quorum())
}

func TestConfig_K(t *testing.T) {
	c := healthyConfig()
	require.Equal(t, 4, c.K())
}

// --- DefaultBroadcastBudget ---

func TestDefaultBroadcastBudget_K4_AtConfigA(t *testing.T) {
	btt := 200 * time.Millisecond
	tVerdictStart := 1600 * time.Millisecond
	out, err := DefaultBroadcastBudget(4, btt, tVerdictStart)
	require.NoError(t, err)
	require.Len(t, out, 4)
	require.Equal(t, 1*btt, out[0])
	require.Equal(t, 150*btt/100, out[1])
	require.Equal(t, 250*btt/100, out[2])
	require.Equal(t, tVerdictStart, out[3])
}

func TestDefaultBroadcastBudget_K3(t *testing.T) {
	btt := 200 * time.Millisecond
	tVerdictStart := 1600 * time.Millisecond
	out, err := DefaultBroadcastBudget(3, btt, tVerdictStart)
	require.NoError(t, err)
	require.Equal(t, []time.Duration{btt, 250 * btt / 100, tVerdictStart}, out)
}

func TestDefaultBroadcastBudget_K5_InterpolatesIntermediate(t *testing.T) {
	btt := 200 * time.Millisecond
	tVerdictStart := 2000 * time.Millisecond
	out, err := DefaultBroadcastBudget(5, btt, tVerdictStart)
	require.NoError(t, err)
	require.Len(t, out, 5)
	require.Equal(t, btt, out[0])
	require.Equal(t, 150*btt/100, out[1])
	require.Equal(t, 250*btt/100, out[2])
	// out[3] interpolates between out[2]=500ms and out[4]=2000ms (one step).
	require.Equal(t, tVerdictStart, out[4])
	require.Greater(t, out[3], out[2])
	require.Less(t, out[3], out[4])
}

// At degraded operating points where TVerdictStart shrinks below the
// canonical staggered multiples, DefaultBroadcastBudget caps shallow
// B_k at TVerdictStart so the schedule stays non-decreasing. The
// capped layers' broadcast targets all clamp at BFT_start at runtime
// (T_broadcast_max_k = max(BFT_start, TVerdictStart − TVerdictStart) =
// BFT_start).
func TestDefaultBroadcastBudget_DegradedOperatingPoint(t *testing.T) {
	btt := 200 * time.Millisecond
	tVerdictStart := 400 * time.Millisecond // < 2.5·BTT
	out, err := DefaultBroadcastBudget(4, btt, tVerdictStart)
	require.NoError(t, err)
	require.Len(t, out, 4)
	require.Equal(t, btt, out[0])           // 200ms — fits
	require.Equal(t, 150*btt/100, out[1])   // 300ms — fits
	require.Equal(t, tVerdictStart, out[2]) // capped (was 500ms, now 400ms)
	require.Equal(t, tVerdictStart, out[3]) // deepest = TVerdictStart
	// Non-decreasing post-cap.
	for k := 1; k < len(out); k++ {
		require.GreaterOrEqual(t, out[k], out[k-1])
	}
}

func TestDefaultBroadcastBudget_K1(t *testing.T) {
	btt := 200 * time.Millisecond
	tVerdictStart := 1600 * time.Millisecond
	out, err := DefaultBroadcastBudget(1, btt, tVerdictStart)
	require.NoError(t, err)
	require.Equal(t, []time.Duration{tVerdictStart}, out,
		"K=1: single layer = deepest = TVerdictStart")
}

// At K=1 the only layer is the deepest = TVerdictStart, regardless of
// whether TVerdictStart satisfies any minimum bound — the helper just
// returns the value verbatim. Liveness-min enforcement happens at
// Config.Validate via the 2·BTT bound on the deepest layer, not in
// the budget helper.
func TestDefaultBroadcastBudget_K1_LowTVerdictStart(t *testing.T) {
	btt := 200 * time.Millisecond
	tVerdictStart := 300 * time.Millisecond // < 2·BTT
	out, err := DefaultBroadcastBudget(1, btt, tVerdictStart)
	require.NoError(t, err)
	require.Equal(t, []time.Duration{tVerdictStart}, out)
}

func TestDefaultBroadcastBudget_K2(t *testing.T) {
	btt := 200 * time.Millisecond
	tVerdictStart := 1600 * time.Millisecond
	out, err := DefaultBroadcastBudget(2, btt, tVerdictStart)
	require.NoError(t, err)
	require.Equal(t, []time.Duration{btt, tVerdictStart}, out,
		"K=2: B_0 = 1 BTT, B_1 = TVerdictStart")
}

// At K=2 with TVerdictStart < BTT, the helper caps B_0 at TVerdictStart
// so both layers tie at the anchor — non-decreasing holds.
func TestDefaultBroadcastBudget_K2_LowTVerdictStart(t *testing.T) {
	btt := 200 * time.Millisecond
	tVerdictStart := 150 * time.Millisecond // < BTT
	out, err := DefaultBroadcastBudget(2, btt, tVerdictStart)
	require.NoError(t, err)
	require.Equal(t, []time.Duration{tVerdictStart, tVerdictStart}, out)
}

func TestDefaultBroadcastBudget_K0_Rejected(t *testing.T) {
	_, err := DefaultBroadcastBudget(0, 200*time.Millisecond, 1600*time.Millisecond)
	require.ErrorContains(t, err, "K=0")
}

func TestDefaultBroadcastBudget_NonPositiveBTT_Rejected(t *testing.T) {
	_, err := DefaultBroadcastBudget(4, 0, 1600*time.Millisecond)
	require.ErrorContains(t, err, "BTT")
}

// --- NewInstance ---

// newInstanceForConfig is a helper for Config-focused tests that need a
// minimally-wired Instance. Sim-level tests use newSim instead (it wires
// every cluster member's Instance + stub deps in one shot).
func newInstanceForConfig(t *testing.T, c *Config, ownOp OperatorID) (*Instance, error) {
	t.Helper()
	if c == nil {
		return NewInstance(nil, ownOp, nil, nil, nil, nil, nil, nil, nil)
	}
	pubShares := make(map[OperatorID][]byte, len(c.Operators))
	for _, op := range c.Operators {
		pubShares[op] = []byte{byte(op)}
	}
	return NewInstance(
		c, ownOp,
		NewStubSigner(c.QV(), pubShares[ownOp]),
		NewStubSigner(c.QV(), pubShares[ownOp]),
		NewStubIBE(c.QEnc()),
		nil, // clusterPubKey — stub ignores
		pubShares,
		nil, // ibePubKeyShares — Option A
		nil, // evidenceObserver
	)
}

func TestNewInstance_RejectsNilConfig(t *testing.T) {
	_, err := newInstanceForConfig(t, nil, 1)
	require.ErrorIs(t, err, ErrNilConfig)
}

func TestNewInstance_RejectsInvalidConfig(t *testing.T) {
	c := healthyConfig()
	c.F = 0 // invalid
	_, err := newInstanceForConfig(t, c, 1)
	require.ErrorContains(t, err, "byzantine bound F")
}

func TestNewInstance_HealthyConfigAccepted(t *testing.T) {
	c := healthyConfig()
	inst, err := newInstanceForConfig(t, c, 1)
	require.NoError(t, err)
	require.Equal(t, c, inst.Config())
	require.Equal(t, OperatorID(1), inst.OwnOperatorID())
}

func TestNewInstance_RejectsNilSigner(t *testing.T) {
	c := healthyConfig()
	_, err := NewInstance(c, 1, nil, nil, NewStubIBE(c.QEnc()), nil, map[OperatorID][]byte{}, nil, nil)
	require.ErrorContains(t, err, "nil signer or ibe")
}

func TestNewInstance_RejectsNilIBE(t *testing.T) {
	c := healthyConfig()
	signer := NewStubSigner(c.QV(), nil)
	_, err := NewInstance(c, 1, signer, signer, nil, nil, map[OperatorID][]byte{}, nil, nil)
	require.ErrorContains(t, err, "nil signer or ibe")
}

func TestNewInstance_RejectsNilPubKeyShares(t *testing.T) {
	c := healthyConfig()
	signer := NewStubSigner(c.QV(), nil)
	_, err := NewInstance(c, 1, signer, signer, NewStubIBE(c.QEnc()), nil, nil, nil, nil)
	require.ErrorContains(t, err, "nil pubKeyShares")
}

func TestNewInstance_RejectsOwnOpNotInCluster(t *testing.T) {
	c := healthyConfig()
	pubShares := map[OperatorID][]byte{1: {1}, 2: {2}, 3: {3}, 4: {4}}
	signer := NewStubSigner(c.QV(), nil)
	_, err := NewInstance(c, 99, signer, signer, NewStubIBE(c.QEnc()), nil, pubShares, nil, nil)
	require.ErrorContains(t, err, "not in cluster")
}

func TestNewInstance_RejectsMissingPubKeyShare(t *testing.T) {
	c := healthyConfig()
	// Missing the share for op 4.
	pubShares := map[OperatorID][]byte{1: {1}, 2: {2}, 3: {3}}
	signer := NewStubSigner(c.QV(), nil)
	_, err := NewInstance(c, 1, signer, signer, NewStubIBE(c.QEnc()), nil, pubShares, nil, nil)
	require.ErrorContains(t, err, "no pub-key share")
}

func TestNewInstance_TagSignerDefaultsToSigner(t *testing.T) {
	// When tagSigner == nil, NewInstance reuses signer (Option A).
	c := healthyConfig()
	pubShares := map[OperatorID][]byte{1: {1}, 2: {2}, 3: {3}, 4: {4}}
	signer := NewStubSigner(c.QV(), nil)
	inst, err := NewInstance(c, 1, signer, nil, NewStubIBE(c.QEnc()), nil, pubShares, nil, nil)
	require.NoError(t, err)
	require.NotNil(t, inst)
}
