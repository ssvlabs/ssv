package consensustest_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	ct "github.com/ssvlabs/ssv/protocol/v2/consensustest"
	obftadapter "github.com/ssvlabs/ssv/protocol/v2/consensustest/obft"
	qbftadapter "github.com/ssvlabs/ssv/protocol/v2/consensustest/qbft"
)

// TestSmoke_HealthyOBFT verifies the OBFT adapter produces a healthy outcome
// on ByzNone. Sanity check that the framework wiring works end-to-end.
func TestSmoke_HealthyOBFT(t *testing.T) {
	cfg := ct.DefaultProposerDutyConfig(200 * time.Millisecond)
	out, err := obftadapter.Protocol{}.Run(cfg)
	require.NoError(t, err)
	require.True(t, out.Decided, "OBFT should decide healthy")
	require.Equal(t, 0, out.DecidedRound, "should decide at L_0")
	t.Logf("OBFT healthy: decided at %v on layer %d, value=%x",
		out.DecisionTime, out.DecidedRound, out.DecidedValue[:6])
}

// TestSmoke_HealthyQBFT verifies the QBFT adapter on ByzNone.
func TestSmoke_HealthyQBFT(t *testing.T) {
	cfg := ct.DefaultProposerDutyConfig(200 * time.Millisecond)
	out, err := qbftadapter.QBFTNoReflood{}.Run(cfg)
	require.NoError(t, err)
	require.True(t, out.Decided, "QBFT should decide healthy")
	require.Equal(t, 0, out.DecidedRound, "should decide at round 1 (= 0-indexed)")
	t.Logf("QBFT healthy: decided at %v on round %d, value=%s",
		out.DecisionTime, out.DecidedRound, string(out.DecidedValue))
}

// TestSmoke_SilentLeaderOBFT — primary OBFT leader silent → fall-through to L_1.
func TestSmoke_SilentLeaderOBFT(t *testing.T) {
	cfg := ct.DefaultProposerDutyConfig(200 * time.Millisecond)
	cfg.Byz = ct.ByzPattern{Kind: ct.ByzSilentLeader, ByzOperators: []ct.OperatorID{1}}
	out, err := obftadapter.Protocol{}.Run(cfg)
	require.NoError(t, err)
	require.True(t, out.Decided, "OBFT should fall through")
	require.Greater(t, out.DecidedRound, 0, "should be deeper layer")
}

// TestSmoke_SilentLeaderQBFT — round-1 leader silent → R2 success.
func TestSmoke_SilentLeaderQBFT(t *testing.T) {
	cfg := ct.DefaultProposerDutyConfig(200 * time.Millisecond)
	cfg.Byz = ct.ByzPattern{Kind: ct.ByzSilentLeader, ByzOperators: []ct.OperatorID{1}}
	out, err := qbftadapter.QBFTNoReflood{}.Run(cfg)
	require.NoError(t, err)
	require.True(t, out.Decided, "QBFT should round-change to R2")
	require.Equal(t, 1, out.DecidedRound, "should decide at round 2 (= 1-indexed → 1)")
	t.Logf("QBFT R2: decided at %v", out.DecisionTime)
}

// TestSmoke_SafetyInvariant — verify that a healthy run produces a clean
// SafetyReport (single V, terminated, agreement). Stress-tests the universal
// invariants check.
func TestSmoke_SafetyInvariant(t *testing.T) {
	cfg := ct.DefaultProposerDutyConfig(200 * time.Millisecond)
	out, err := obftadapter.Protocol{}.Run(cfg)
	require.NoError(t, err)
	report := ct.ComputeSafetyReport(out)
	require.True(t, report.SingleV, "SingleV must hold: %s", report)
	require.True(t, report.HonestAgreement, "HonestAgreement must hold: %s", report)
	require.LessOrEqual(t, len(report.DistinctOutputs), 1)
}

// TestSmoke_NotApplicable — QBFT should reject OBFT-specific byz patterns
// that have no QBFT analog. ByzFakeEncryptedPresence is one such kind: it
// relies on the OBFT chained-onion σ-bundle encryption, which QBFT doesn't
// have. (Earlier ByzHV1SelectiveDelivery worked here too but the QBFT
// adapter now translates it to a selective-delivery byz model — see
// qbft/byz.go.)
func TestSmoke_NotApplicable(t *testing.T) {
	cfg := ct.DefaultProposerDutyConfig(200 * time.Millisecond)
	cfg.Byz = ct.ByzPattern{Kind: ct.ByzFakeEncryptedPresence, ByzOperators: []ct.OperatorID{1}}
	_, err := qbftadapter.QBFTNoReflood{}.Run(cfg)
	require.ErrorIs(t, err, ct.ErrNotApplicable)
}

// TestSmoke_RunScenarioOnProtocol — verify the framework's universal-invariant
// runner works.
func TestSmoke_RunScenarioOnProtocol(t *testing.T) {
	base := ct.DefaultProposerDutyConfig(200 * time.Millisecond)
	scenario := ct.Scenario{
		Name:  "Healthy",
		Apply: func(c *ct.SimConfig) { c.Byz = ct.ByzPattern{Kind: ct.ByzNone} },
		Expect: map[string]ct.ExpectClass{
			"OBFT": ct.ExpectSuccessFastest,
			"QBFT": ct.ExpectSuccessFastest,
		},
	}
	for _, p := range []ct.Protocol{obftadapter.Protocol{}, qbftadapter.QBFTNoReflood{}} {
		r := ct.RunScenarioOnProtocol(t, p, scenario, base)
		require.Truef(t, r.Match, "%s/%s mismatch: %s", p.Name(), scenario.Name, r.Why)
		require.Truef(t, r.Safety.SingleV, "%s/%s safety: %s", p.Name(), scenario.Name, r.Safety)
	}
}

// TestSmoke_RealBLS — exercises the real-BLS / TLockIBE crypto path end-to-end
// through the OBFT adapter on a healthy run. Without this, real-BLS wiring
// could regress unnoticed (the cross-protocol scenarios all use stub crypto).
func TestSmoke_RealBLS(t *testing.T) {
	cfg := ct.DefaultProposerDutyConfig(200 * time.Millisecond)
	keys, err := ct.GenerateBLSKeys(cfg.Operators)
	require.NoError(t, err)
	cfg.BLSKeys = keys

	out, err := obftadapter.Protocol{}.Run(cfg)
	require.NoError(t, err)
	require.True(t, out.Decided, "OBFT real-BLS healthy must decide")
	require.Equal(t, 0, out.DecidedRound, "should decide at L_0 fastest path")
}

// TestSmoke_TraceDeterministic — re-running the same (cfg, seed) twice must
// produce byte-identical trace AND byte-identical Outcome. Stress-tests the
// framework's determinism invariant (single-goroutine event loop + (timestamp,
// sequence) ordering); a non-deterministic map iteration somewhere in the
// outcome construction would slip past a trace-only check.
func TestSmoke_TraceDeterministic(t *testing.T) {
	cfg := ct.DefaultProposerDutyConfig(200 * time.Millisecond)
	cfg.Byz = ct.ByzPattern{Kind: ct.ByzSilentLeader, ByzOperators: []ct.OperatorID{1}}
	cfg.TraceEnabled = true

	for _, p := range []ct.Protocol{obftadapter.Protocol{}, qbftadapter.QBFTNoReflood{}} {
		out1, err := p.Run(cfg)
		require.NoError(t, err)
		out2, err := p.Run(cfg)
		require.NoError(t, err)
		require.Equalf(t, out1.Decided, out2.Decided, "%s Decided differs", p.Name())
		require.Equalf(t, out1.DecisionTime, out2.DecisionTime, "%s DecisionTime differs", p.Name())
		require.Equalf(t, out1.DecidedRound, out2.DecidedRound, "%s DecidedRound differs", p.Name())
		require.Equalf(t, out1.DecidedValue, out2.DecidedValue, "%s DecidedValue differs", p.Name())
		require.Equalf(t, out1.PerOp, out2.PerOp, "%s PerOp differs", p.Name())
		require.Equalf(t, len(out1.Trace), len(out2.Trace), "%s trace length differs", p.Name())
		for i := range out1.Trace {
			require.Equalf(t, out1.Trace[i], out2.Trace[i],
				"%s trace[%d] differs across runs", p.Name(), i)
		}
	}
}
