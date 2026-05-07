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

	out, err := qbftadapter.Protocol{}.Run(cfg)
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

	out, err := qbftadapter.Protocol{}.Run(cfg)
	require.NoError(t, err)
	require.True(t, out.Decided, "QBFT should decide at R2 fresh-V; got !Decided")
	require.Equal(t, 1, out.DecidedRound, "should decide at R2 (= 1-indexed → 1)")
}
