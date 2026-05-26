package twoab

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// healthyConfig returns a spec-conforming Config at the SSV default
// operating point (n=4, f=1, K=2, BTT=200ms, SafetyBuffer=SafetyBuffer=700ms).
func healthyConfig() *Config {
	const (
		btt          = 200 * time.Millisecond
		safetyBuffer = 700 * time.Millisecond
		tPhase2a     = 2200 * time.Millisecond
	)
	t0Broadcast := tPhase2a - btt
	K := 2
	budgets, _ := DefaultBroadcastBudget(K, btt, t0Broadcast)
	layers := make([]LayerSpec, K)
	for k := 0; k < K; k++ {
		fetchAt := t0Broadcast - budgets[k]
		if fetchAt < 0 {
			fetchAt = 0
		}
		layers[k] = LayerSpec{
			Leader:          OperatorID(k + 1),
			FetchAt:         fetchAt,
			BroadcastBudget: budgets[k],
		}
	}
	return &Config{
		Height:       1,
		ClusterID:    [32]byte{0xaa, 0xbb, 0xcc},
		Operators:    []OperatorID{1, 2, 3, 4},
		F:            1,
		Layers:       layers,
		TPhase2a:     tPhase2a,
		SafetyBuffer: safetyBuffer,
		BTT:          btt,
	}
}

func TestConfig_K(t *testing.T) {
	c := healthyConfig()
	require.Equal(t, 2, c.K())
}

func TestConfig_QV_QEnc_Quorum(t *testing.T) {
	c := healthyConfig()
	require.Equal(t, 3, c.QV())
	require.Equal(t, 3, c.QEnc())
	require.Equal(t, 3, c.Quorum())
}

func TestConfig_T0Broadcast(t *testing.T) {
	c := healthyConfig()
	require.Equal(t, c.TPhase2a-c.BTT, c.T0Broadcast())
}

func TestConfig_Validate_HealthyConfigAccepted(t *testing.T) {
	require.NoError(t, healthyConfig().Validate())
}

func TestConfig_Validate_RejectsZeroF(t *testing.T) {
	c := healthyConfig()
	c.F = 0
	require.Error(t, c.Validate())
}

func TestConfig_Validate_RejectsClusterBelow3Fplus1(t *testing.T) {
	c := healthyConfig()
	c.Operators = []OperatorID{1, 2, 3}
	require.Error(t, c.Validate())
}

func TestConfig_Validate_RejectsKBelowBFTLivenessMinimum(t *testing.T) {
	c := healthyConfig()
	c.Layers = c.Layers[:1] // K=1 at f=1 is below the f+1 minimum
	require.Error(t, c.Validate())
}

func TestConfig_Validate_RejectsKAboveClusterSize(t *testing.T) {
	c := healthyConfig()
	// 5 layers with 4-op cluster.
	extra := LayerSpec{Leader: OperatorID(5), FetchAt: 0, BroadcastBudget: time.Second}
	c.Layers = append(c.Layers, extra, extra, extra)
	require.Error(t, c.Validate())
}

func TestConfig_Validate_RejectsNonPositiveBTT(t *testing.T) {
	c := healthyConfig()
	c.BTT = 0
	require.Error(t, c.Validate())
}

func TestConfig_Validate_RejectsNonPositiveTPhase2a(t *testing.T) {
	c := healthyConfig()
	c.TPhase2a = 0
	require.Error(t, c.Validate())
}

func TestConfig_Validate_RejectsNegativeSafetyBuffer(t *testing.T) {
	c := healthyConfig()
	c.SafetyBuffer = -1
	require.Error(t, c.Validate())
}

func TestConfig_Validate_AcceptsZeroSafetyBuffer(t *testing.T) {
	c := healthyConfig()
	// Recompute budgets with SafetyBuffer=0 so the budget shape stays valid.
	c.SafetyBuffer = 0
	budgets, err := DefaultBroadcastBudget(c.K(), c.BTT, c.T0Broadcast())
	require.NoError(t, err)
	for k := range c.Layers {
		c.Layers[k].BroadcastBudget = budgets[k]
		fetchAt := c.T0Broadcast() - budgets[k]
		if fetchAt < 0 {
			fetchAt = 0
		}
		c.Layers[k].FetchAt = fetchAt
	}
	require.NoError(t, c.Validate())
}

func TestConfig_Validate_RejectsTPhase2aBelowBTT(t *testing.T) {
	c := healthyConfig()
	c.TPhase2a = c.BTT
	require.Error(t, c.Validate())
}

func TestConfig_Validate_RejectsDuplicateOperator(t *testing.T) {
	c := healthyConfig()
	c.Operators = []OperatorID{1, 1, 3, 4}
	require.Error(t, c.Validate())
}

func TestConfig_Validate_RejectsDuplicateLayerLeader(t *testing.T) {
	c := healthyConfig()
	c.Layers[1].Leader = c.Layers[0].Leader
	require.Error(t, c.Validate())
}

func TestConfig_Validate_RejectsLayerLeaderNotInCluster(t *testing.T) {
	c := healthyConfig()
	c.Layers[0].Leader = 99
	require.Error(t, c.Validate())
}

func TestConfig_Validate_RejectsLayerWithZeroBroadcastBudget(t *testing.T) {
	c := healthyConfig()
	c.Layers[0].BroadcastBudget = 0
	require.Error(t, c.Validate())
}

func TestConfig_Validate_RejectsDecreasingBroadcastBudgets(t *testing.T) {
	c := healthyConfig()
	c.Layers[0].BroadcastBudget = c.Layers[1].BroadcastBudget + 1
	require.Error(t, c.Validate())
}

func TestConfig_Validate_AcceptsEqualAdjacentBroadcastBudgets(t *testing.T) {
	c := healthyConfig()
	// Equal budgets force both layers' broadcast deadline to clamp at
	// slot start (0). Adjust FetchAt to match (must be ≤ broadcast deadline).
	c.Layers[0].BroadcastBudget = c.Layers[1].BroadcastBudget
	c.Layers[0].FetchAt = 0
	c.Layers[1].FetchAt = 0
	require.NoError(t, c.Validate())
}

func TestConfig_Validate_RejectsIncreasingFetchAt(t *testing.T) {
	c := healthyConfig()
	c.Layers[0].FetchAt = 0
	c.Layers[1].FetchAt = c.Layers[0].FetchAt + time.Millisecond
	require.Error(t, c.Validate())
}

func TestDefaultBroadcastBudget_K2_AtConfigA(t *testing.T) {
	budgets, err := DefaultBroadcastBudget(2, 200*time.Millisecond, 2000*time.Millisecond)
	require.NoError(t, err)
	require.Len(t, budgets, 2)
	require.Equal(t, 2*200*time.Millisecond, budgets[0])
	require.Equal(t, 2000*time.Millisecond, budgets[1])
}

func TestDefaultBroadcastBudget_K4_AtConfigA(t *testing.T) {
	budgets, err := DefaultBroadcastBudget(4, 200*time.Millisecond, 2000*time.Millisecond)
	require.NoError(t, err)
	require.Len(t, budgets, 4)
	require.Equal(t, 2*200*time.Millisecond, budgets[0])
	require.Equal(t, 3*200*time.Millisecond, budgets[1])
	require.Equal(t, 4*200*time.Millisecond, budgets[2])
	require.Equal(t, 2000*time.Millisecond, budgets[3])
}

func TestDefaultBroadcastBudget_K1(t *testing.T) {
	// K=1: only the deepest layer; B_0 = T0Broadcast (anchor only).
	budgets, err := DefaultBroadcastBudget(1, 200*time.Millisecond, 2*time.Second)
	require.NoError(t, err)
	require.Len(t, budgets, 1)
	require.Equal(t, 2*time.Second, budgets[0])
}

func TestDefaultBroadcastBudget_K3(t *testing.T) {
	// K=3: B_0 = 2·BTT, B_1 = 3·BTT, B_2 = T0Broadcast.
	budgets, err := DefaultBroadcastBudget(3, 200*time.Millisecond, 2000*time.Millisecond)
	require.NoError(t, err)
	require.Len(t, budgets, 3)
	require.Equal(t, 2*200*time.Millisecond, budgets[0])
	require.Equal(t, 3*200*time.Millisecond, budgets[1])
	require.Equal(t, 2000*time.Millisecond, budgets[2])
}

func TestDefaultBroadcastBudget_K5_InterpolatesIntermediate(t *testing.T) {
	// K=5 exercises the K>4 default branch: shallow layers 0,1,2 at
	// (k+2)·BTT; deepest at T0Broadcast; intermediate layers (k=3
	// here, since K-2=3) interpolate linearly in duration space from
	// layer-2's value to T0Broadcast.
	const (
		btt         = 200 * time.Millisecond
		t0Broadcast = 4 * time.Second
	)
	budgets, err := DefaultBroadcastBudget(5, btt, t0Broadcast)
	require.NoError(t, err)
	require.Len(t, budgets, 5)
	require.Equal(t, 2*btt, budgets[0]) // 400ms
	require.Equal(t, 3*btt, budgets[1]) // 600ms
	require.Equal(t, 4*btt, budgets[2]) // 800ms
	// Interpolated layer (k=3): out[2] + span * (k-2) / steps
	// out[2] = 800ms; span = 4s - 800ms = 3.2s; steps = K-3 = 2; k-2 = 1
	// → out[3] = 800ms + 3.2s * 1/2 = 800ms + 1.6s = 2.4s.
	require.Equal(t, 2400*time.Millisecond, budgets[3])
	require.Equal(t, t0Broadcast, budgets[4])
	// Non-decreasing
	for k := 1; k < 5; k++ {
		require.GreaterOrEqual(t, budgets[k], budgets[k-1], "B_%d should be >= B_%d", k, k-1)
	}
}

func TestDefaultBroadcastBudget_K6_InterpolatesTwoIntermediate(t *testing.T) {
	// K=6: two intermediate layers (k=3 and k=4) interpolate between
	// shallow layer 2 and deepest layer K-1=5.
	const (
		btt         = 200 * time.Millisecond
		t0Broadcast = 5 * time.Second
	)
	budgets, err := DefaultBroadcastBudget(6, btt, t0Broadcast)
	require.NoError(t, err)
	require.Len(t, budgets, 6)
	// Both intermediate layers strictly between shallow-2 and deepest.
	require.Greater(t, budgets[3], budgets[2])
	require.Greater(t, budgets[4], budgets[3])
	require.Equal(t, t0Broadcast, budgets[5])
}

func TestDefaultBroadcastBudget_RejectsK0(t *testing.T) {
	_, err := DefaultBroadcastBudget(0, 200*time.Millisecond, 2*time.Second)
	require.Error(t, err)
}

func TestDefaultBroadcastBudget_RejectsNonPositiveBTT(t *testing.T) {
	_, err := DefaultBroadcastBudget(2, 0, 2*time.Second)
	require.Error(t, err)
}

func TestDefaultBroadcastBudget_DegradedT0BroadcastClampsBudgets(t *testing.T) {
	// T0Broadcast smaller than shallow B_0 = 2·BTT: budgets cap at
	// T0Broadcast.
	const (
		btt         = 200 * time.Millisecond
		t0Broadcast = 300 * time.Millisecond // shallow B_0 = 400ms > T0Broadcast
	)
	budgets, err := DefaultBroadcastBudget(2, btt, t0Broadcast)
	require.NoError(t, err)
	require.Len(t, budgets, 2)
	require.Equal(t, t0Broadcast, budgets[0])
	require.Equal(t, t0Broadcast, budgets[1])
}
