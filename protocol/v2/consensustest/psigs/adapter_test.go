package psigs_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	ct "github.com/ssvlabs/ssv/protocol/v2/consensustest"
	psigsadapter "github.com/ssvlabs/ssv/protocol/v2/consensustest/psigs"
)

// TestMeshArrival_NoRefloodToPublisher mirrors the OBFT regression
// test. See its docstring for the design rationale.
func TestMeshArrival_NoRefloodToPublisher(t *testing.T) {
	cfg := ct.DefaultProposerDutyConfig(200 * time.Millisecond)
	cfg.Delivery = ct.DeliveryMesh
	cfg.TraceEnabled = true
	out, err := psigsadapter.Protocol{}.Run(cfg)
	require.NoError(t, err)
	require.True(t, out.Decided, "mesh-mode healthy should decide")
	ct.AssertNoRefloodToPublisher(t, out.Trace)
}

// TestAdapter_Healthy — baseline: every honest operator signs at
// BFTStart, partial-sigs propagate via ConstantDelay(BTT), and σ-quorum
// reaches at the qV-th arrival. At n=4 / f=1 / qV=3, each receiver
// already self-counts (1) and gains one partial per ConstantDelay tick.
// All n-1 = 3 peer partials land simultaneously at BFTStart + BTT, so
// every op decides at that moment.
func TestAdapter_Healthy(t *testing.T) {
	cfg := ct.DefaultProposerDutyConfig(200 * time.Millisecond)
	out, err := psigsadapter.Protocol{}.Run(cfg)
	require.NoError(t, err)
	require.True(t, out.Decided, "healthy should decide")
	require.Equal(t, 0, out.DecidedRound, "PSigs single-round → DecidedRound=0")
	// At BTT=200ms with BFTStart=0 and qV=3 / n=4, every op needs 2
	// peer partials (own count = 1). Under ConstantDelay all peer
	// partials arrive at exactly 200ms, so all 4 ops decide at 200ms.
	require.Equal(t, 200*time.Millisecond, out.DecisionTime,
		"healthy DecisionTime = BFTStart + 1·BTT (partials arrive synchronously)")
}

// TestAdapter_Name — Protocol{}.Name() defaults to "PSigs"; VariantName
// override produces the supplied name.
func TestAdapter_Name(t *testing.T) {
	require.Equal(t, "PSigs", psigsadapter.Protocol{}.Name())
	require.Equal(t, "PSigs-X", psigsadapter.Protocol{VariantName: "PSigs-X"}.Name())
}

// TestAdapter_SigmaRefusal_WithinFBound — one byz operator refuses to
// sign; the cluster still reaches qV from the remaining 3 honest ops
// (own + 2 peers = 3 = qV). Healthy DecisionTime still holds since the
// last needed partial arrives at BFTStart + 1·BTT.
func TestAdapter_SigmaRefusal_WithinFBound(t *testing.T) {
	cfg := ct.DefaultProposerDutyConfig(200 * time.Millisecond)
	cfg.Byz = ct.ByzPattern{Kind: ct.ByzSigmaRefusal, ByzOperators: []ct.OperatorID{4}}

	out, err := psigsadapter.Protocol{}.Run(cfg)
	require.NoError(t, err)
	require.True(t, out.Decided, "single byz refusal stays within f-bound; cluster decides")
	require.Equal(t, 0, out.DecidedRound)
	// Honest ops (1, 2, 3) each have own=1 + 2 peer partials = 3 = qV at
	// BFTStart + 1·BTT. The byz op (4) doesn't self-sign (own=0) but
	// still receives the 3 honest partials at BTT, so its local count
	// also reaches 3 = qV; the byz "decides" too in the framework's
	// could-aggregate semantic (mirrors OBFT's byzRefuse — a refusing
	// byz can still observe and aggregate if they wanted to).
	require.Equal(t, 200*time.Millisecond, out.DecisionTime,
		"healthy-equivalent DecisionTime — qV reached at BFTStart + 1·BTT")
}

// TestAdapter_NotApplicable_Equivocation — equivocation patterns have
// no PSigs analog (no leader to equivocate); the adapter returns
// ErrNotApplicable so the catalog cell renders n/a.
func TestAdapter_NotApplicable_Equivocation(t *testing.T) {
	cfg := ct.DefaultProposerDutyConfig(200 * time.Millisecond)
	cfg.Byz = ct.ByzPattern{Kind: ct.ByzEquivocate111, ByzOperators: []ct.OperatorID{1}}

	_, err := psigsadapter.Protocol{}.Run(cfg)
	require.ErrorIs(t, err, ct.ErrNotApplicable,
		"PSigs has no leader to equivocate; must surface ErrNotApplicable")
}

// TestAdapter_DelayedCommit_LandsInTime — byz signs but with extra
// dispatch delay (1.5·BTT past the honest arrival). The 3 honest ops
// (1, 2, 3) still reach qV at BFTStart + 1·BTT via the honest peer
// partials alone; the byz's late partial arrives later and is
// irrelevant to the decision time.
func TestAdapter_DelayedCommit_LandsInTime(t *testing.T) {
	cfg := ct.DefaultProposerDutyConfig(200 * time.Millisecond)
	cfg.Byz = ct.ByzPattern{Kind: ct.ByzDelayedCommit, ByzOperators: []ct.OperatorID{4}}

	out, err := psigsadapter.Protocol{}.Run(cfg)
	require.NoError(t, err)
	require.True(t, out.Decided)
	require.Equal(t, 0, out.DecidedRound)
	require.Equal(t, 200*time.Millisecond, out.DecisionTime,
		"3 honest partials at BFTStart + 1·BTT meet qV without needing the delayed byz partial")
}

// TestAdapter_LateBFTStart_ClipsToMiss — when BFTStart is late enough
// that even the optimistic BFTStart + 1·BTT decision lands past
// RelayCutoff − HeaderSubmitHeadroom, ClipLateDecision converts the
// outcome to MISS. Pins that PSigs honors the same submit-deadline
// semantic as OBFT / QBFT (the heatmap's BFT_start picker uses this
// same clipping at every adapter).
func TestAdapter_LateBFTStart_ClipsToMiss(t *testing.T) {
	cfg := ct.DefaultProposerDutyConfig(200 * time.Millisecond)
	// Deadline = RelayCutoff − HeaderSubmitHeadroom = 4000 − 100 = 3900ms.
	// BFTStart = 3800ms → optimistic decision at 4000ms > 3900ms → miss.
	cfg.BFTStart = 3800 * time.Millisecond
	out, err := psigsadapter.Protocol{}.Run(cfg)
	require.NoError(t, err)
	require.False(t, out.Decided,
		"BFTStart=3800ms pushes the BTT-delayed quorum past the 3900ms submit deadline → must clip to MISS")
	require.Equal(t, -1, out.DecidedRound, "clipped MISS reports DecidedRound = -1")
}

// TestAdapter_DeliveryMesh_Healthy — exercises the mesh transport path
// (evtMeshArrival re-flood). Asserts that the mesh-mode run still
// decides at L_0 under all-honest defaults, with PerOperatorIn[0]
// staying zero (relay bandwidth must not pollute the per-op histogram).
func TestAdapter_DeliveryMesh_Healthy(t *testing.T) {
	cfg := ct.DefaultProposerDutyConfig(200 * time.Millisecond)
	cfg.Delivery = ct.DeliveryMesh
	out, err := psigsadapter.Protocol{}.Run(cfg)
	require.NoError(t, err)
	require.True(t, out.Decided, "mesh-mode healthy should decide")
	require.Equal(t, 0, out.DecidedRound)
	require.Zero(t, out.Bandwidth.PerOperatorIn[0],
		"PerOperatorIn[0] must stay zero — relay-bound bytes should not pollute the per-operator histogram")
	for op := ct.OperatorID(1); op <= 4; op++ {
		require.Positive(t, out.Bandwidth.PerOperatorIn[op],
			"cluster operator %d expected non-zero PerOperatorIn", op)
	}
}

// TestAdapter_PerOpBandwidth_Symmetric — under DeliveryDirect every op
// emits one partial-sig to each of its n-1 peers. Bandwidth accounting
// should reflect that (each op has the same Out, and In = 3 × pSigBytes
// from 3 peers).
func TestAdapter_PerOpBandwidth_Symmetric(t *testing.T) {
	cfg := ct.DefaultProposerDutyConfig(200 * time.Millisecond)
	out, err := psigsadapter.Protocol{}.Run(cfg)
	require.NoError(t, err)
	// In a 4-op healthy run, each op emits n-1 = 3 partials and receives
	// n-1 = 3 partials. Out and In are bytewise symmetric across the
	// cluster.
	var refOut, refIn int64
	for op := ct.OperatorID(1); op <= 4; op++ {
		if refOut == 0 {
			refOut = out.Bandwidth.PerOperatorOut[op]
			refIn = out.Bandwidth.PerOperatorIn[op]
			continue
		}
		require.Equal(t, refOut, out.Bandwidth.PerOperatorOut[op],
			"op %d Out differs from reference (PSigs is bandwidth-symmetric)", op)
		require.Equal(t, refIn, out.Bandwidth.PerOperatorIn[op],
			"op %d In differs from reference (PSigs is bandwidth-symmetric)", op)
	}
	require.Positive(t, refOut, "every op must emit at least one partial-sig wire byte")
}
