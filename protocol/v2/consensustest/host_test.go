package consensustest_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	ct "github.com/ssvlabs/ssv/protocol/v2/consensustest"
	obftadapter "github.com/ssvlabs/ssv/protocol/v2/consensustest/obft"
	qbftadapter "github.com/ssvlabs/ssv/protocol/v2/consensustest/qbft"
)

// TestHost_FlipMidSlot_OBFT_DecidesAtL0 — host valid only at L_0; healthy
// path holds (ops σ-emit at L_0, σ-quorum reaches, slot decides without
// needing the deeper layers' "invalid" verdict).
func TestHost_FlipMidSlot_OBFT_DecidesAtL0(t *testing.T) {
	cfg := ct.DefaultProposerDutyConfig(200 * time.Millisecond)
	cfg.Host = ct.HostFlipMidSlot{ValidUntilLayer: 0}

	out, err := obftadapter.Protocol{}.Run(cfg)
	require.NoError(t, err)
	require.True(t, out.Decided, "OBFT should decide at L_0 (host valid there)")
	require.Equal(t, 0, out.DecidedRound, "should decide at fastest path")
}

// TestHost_FlipMidSlot_QBFT_RoundAwareReject — round 1 host-valid, round 2
// host-invalid. Pair with byz-silent round-1 leader so cluster round-changes
// to R2; R2 PROPOSE rejected by host → cluster MISSES. This exercises the
// round-aware ValueChecker (Phase 2 self-review fix) end-to-end: without
// per-round host queries, R2 would silently succeed and the test's MISS
// expectation would fail.
func TestHost_FlipMidSlot_QBFT_RoundAwareReject(t *testing.T) {
	cfg := ct.DefaultProposerDutyConfig(200 * time.Millisecond)
	cfg.Byz = ct.ByzPattern{Kind: ct.ByzSilentLeader, ByzOperators: []ct.OperatorID{1}}
	cfg.Host = ct.HostFlipMidSlot{ValidUntilLayer: 0} // R1 valid (= layer 0); R2 (= layer 1) rejected

	out, err := qbftadapter.QBFT0{}.Run(cfg)
	require.NoError(t, err)
	require.False(t, out.Decided,
		"QBFT R1 silenced + R2 host-invalid should MISS; got decided at round %d", out.DecidedRound)
}

// TestHost_InvalidUntilLayer_OBFT_DecidesAtL1 — host invalid at L_0, valid
// at L_1+. Ops NR at L_0 (host rejected V_0), NR-quorum unlocks L_1, ops
// σ-emit at L_1, slot decides at L_1.
func TestHost_InvalidUntilLayer_OBFT_DecidesAtL1(t *testing.T) {
	cfg := ct.DefaultProposerDutyConfig(200 * time.Millisecond)
	cfg.Host = ct.HostInvalidUntilLayer{InvalidUntilLayer: 0}

	out, err := obftadapter.Protocol{}.Run(cfg)
	require.NoError(t, err)
	require.True(t, out.Decided,
		"OBFT should fall through to L_1 (host valid there); got !Decided")
	require.GreaterOrEqual(t, out.DecidedRound, 1,
		"should NOT decide at L_0 (host invalid there); got L_%d", out.DecidedRound)
}

// TestHost_InvalidUntilLayer_QBFT_DecidesAtR2 — host invalid at round 1,
// valid at round 2+. Round 1 PROPOSE host-rejected → no PREPARE → R1 timeout
// → R2 fresh-V PROPOSE host-validates → slot decides at R2.
func TestHost_InvalidUntilLayer_QBFT_DecidesAtR2(t *testing.T) {
	cfg := ct.DefaultProposerDutyConfig(200 * time.Millisecond)
	cfg.Host = ct.HostInvalidUntilLayer{InvalidUntilLayer: 0}

	out, err := qbftadapter.QBFT0{}.Run(cfg)
	require.NoError(t, err)
	require.True(t, out.Decided, "QBFT should decide at R2 fresh-V; got !Decided")
	require.Equal(t, 1, out.DecidedRound, "should decide at R2 (= 1-indexed → 1)")
}

// TestHost_ValidityDivergence_3_1_DecidesAtL0 — minority NV (1 of 4) at L_0.
// σ-pool = 3 valid honest σ + leader's σ_L^V = 4 ≥ qV=3 → slot succeeds at L_0.
// Validates that one dissenter doesn't break the slot under OBFT's σ-quorum
// machinery. Distinct from the 2-2 split (miss) and 1-3 split (fall-through).
func TestHost_ValidityDivergence_3_1_DecidesAtL0(t *testing.T) {
	cfg := ct.DefaultProposerDutyConfig(200 * time.Millisecond)
	cfg.Host = ct.HostInvalidForOperators{
		Layer:     0,
		Operators: map[ct.OperatorID]bool{4: true},
	}

	out, err := obftadapter.Protocol{}.Run(cfg)
	require.NoError(t, err)
	require.True(t, out.Decided, "OBFT 3-1 split should σ-quorum at L_0 with majority valid")
	require.Equal(t, 0, out.DecidedRound, "should decide at L_0 fastest path")

	rep := ct.ComputeSafetyReport(out)
	require.True(t, rep.SingleV, "SingleV: %s", rep)
	require.True(t, rep.NoOfflineDoubleV, "NoOfflineDoubleV: %s", rep)
}

// TestHost_ValidityDivergence_1_3_DecidesAtL1 — majority NV (3 of 4) at L_0.
// σ-pool at L_0 = 1 valid + leader's σ_L^V = 2 < qV=3. NR-pool = 3 NV honest =
// 3 ≥ qEnc=3 → NR-quorum unlocks L_1 → slot succeeds at L_1 (host valid there).
// Validates the host-invalidity fall-through path through NR-quorum.
func TestHost_ValidityDivergence_1_3_DecidesAtL1(t *testing.T) {
	cfg := ct.DefaultProposerDutyConfig(200 * time.Millisecond)
	cfg.Host = ct.HostInvalidForOperators{
		Layer:     0,
		Operators: map[ct.OperatorID]bool{2: true, 3: true, 4: true},
	}

	out, err := obftadapter.Protocol{}.Run(cfg)
	require.NoError(t, err)
	require.True(t, out.Decided, "OBFT 1-3 split should fall through via NR-quorum to L_1")
	require.GreaterOrEqual(t, out.DecidedRound, 1,
		"should NOT decide at L_0 (σ-pool < qV); got L_%d", out.DecidedRound)

	rep := ct.ComputeSafetyReport(out)
	require.True(t, rep.SingleV, "SingleV: %s", rep)
	require.True(t, rep.NoOfflineDoubleV, "NoOfflineDoubleV: %s", rep)
}
