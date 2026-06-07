package goclient

import (
	"context"
	"math/big"
	"testing"
	"time"

	"github.com/attestantio/go-eth2-client/spec/bellatrix"
	"github.com/attestantio/go-eth2-client/spec/phase0"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ssvlabs/ssv/observability/log"
)

// Tests for proposal-collection dispatch: the MEV-optimized slot-relative-deadline strategy and the
// legacy relative-timeout strategy. See docs/MEV_CONSIDERATIONS.md for the semantics.

// TestNew_StoresProposalFetchConfig verifies that the block-fetch timing fields (proposalSoftDeadline
// / proposalSoftTimeout) propagate from Options into the GoClient, and that useSlotRelativeFetch
// derives the path from them. In production these resolved values come from cli/operator config.
func TestNew_StoresProposalFetchConfig(t *testing.T) {
	tests := []struct {
		name             string
		opts             Options // block-fetch timing field only; transport fields are filled in below
		wantSlotRelative bool
	}{
		{
			name:             "mev-optimized (ProposalSoftDeadline set)",
			opts:             Options{ProposalSoftDeadline: 1100 * time.Millisecond},
			wantSlotRelative: true,
		},
		{
			name:             "legacy (ProposalSoftTimeout set)",
			opts:             Options{ProposalSoftTimeout: 1800 * time.Millisecond},
			wantSlotRelative: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server, _ := createProposalBeaconServer(t, beaconProposalServerOptions{})
			defer server.Close()

			base := tt.opts
			base.BeaconNodeAddr = server.URL
			base.CommonTimeout = time.Second * 2
			base.LongTimeout = time.Second * 5

			client, err := New(t.Context(), log.TestLogger(t), base)
			require.NoError(t, err)

			assert.Equal(t, tt.opts.ProposalSoftDeadline, client.proposalSoftDeadline,
				"New should propagate ProposalSoftDeadline")
			assert.Equal(t, tt.opts.ProposalSoftTimeout, client.proposalSoftTimeout,
				"New should propagate ProposalSoftTimeout")
			assert.Equal(t, tt.wantSlotRelative, client.useSlotRelativeFetch(),
				"useSlotRelativeFetch should reflect a positive ProposalSoftDeadline")
		})
	}
}

// TestGetBeaconBlock_MultiBN_MEVOptimized_WaitsUntilDeadline verifies that the MEV-optimized path
// does NOT early-exit on the first blinded response: even with fast blinded BNs it keeps collecting
// until the slot-relative deadline before returning, so QBFT starts at a cluster-aligned slot time.
// (Contrast the legacy path, which early-exits on the first blinded.)
func TestGetBeaconBlock_MultiBN_MEVOptimized_WaitsUntilDeadline(t *testing.T) {
	bn1, _ := createProposalBeaconServer(t, beaconProposalServerOptions{
		ProposalResponseDuration: 10 * time.Millisecond,
		BlindedProposal:          true,
		FeeRecipient:             feeRecipientAllOnes(),
	})
	defer bn1.Close()
	bn2, _ := createProposalBeaconServer(t, beaconProposalServerOptions{
		ProposalResponseDuration: 50 * time.Millisecond,
		BlindedProposal:          true,
		FeeRecipient:             feeRecipientAllTwos(),
	})
	defer bn2.Close()

	client := setupMultiBNClient(t, bn1.URL, bn2.URL, 1500*time.Millisecond)
	const deadlineFromNow = 600 * time.Millisecond
	slot := armProposalDeadline(t, client, deadlineFromNow)

	start := time.Now()
	_, _, err := client.GetBeaconBlock(context.Background(), slot, []byte("test"), getTestRANDAO())
	elapsed := time.Since(start)
	require.NoError(t, err)

	// Both BNs respond within ~50ms, but the floor holds the result until the deadline (~600ms).
	// The lower bound (generous for clock jitter / scheduling) proves we did not early-exit; the
	// upper bound guards against a regression that waits on the wrong (e.g. far-future) deadline.
	assert.GreaterOrEqual(t, elapsed, 450*time.Millisecond,
		"MEV-optimized path should wait until the slot-relative deadline, not early-exit; took %v", elapsed)
	assert.Less(t, elapsed, 1200*time.Millisecond,
		"MEV-optimized path should return at the deadline (~600ms); took %v", elapsed)
}

// TestGetBeaconBlock_MultiBN_MEVOptimized_HighestScoringBlindedWins verifies that when multiple BNs
// return blinded proposals within the collection window, the MEV-optimized path selects the
// highest-scoring one (sum of ConsensusValue + ExecutionValue), not the first-arriving.
func TestGetBeaconBlock_MultiBN_MEVOptimized_HighestScoringBlindedWins(t *testing.T) {
	bn1, _ := createProposalBeaconServer(t, beaconProposalServerOptions{
		ProposalResponseDuration: 10 * time.Millisecond,
		BlindedProposal:          true,
		FeeRecipient:             feeRecipientAllOnes(),
		ExecutionValue:           big.NewInt(1_000_000), // low bid
	})
	defer bn1.Close()
	bn2, _ := createProposalBeaconServer(t, beaconProposalServerOptions{
		ProposalResponseDuration: 200 * time.Millisecond,
		BlindedProposal:          true,
		FeeRecipient:             feeRecipientAllTwos(),
		ExecutionValue:           big.NewInt(5_000_000), // high bid (must win)
	})
	defer bn2.Close()

	client := setupMultiBNClient(t, bn1.URL, bn2.URL, 1500*time.Millisecond)
	slot := armProposalDeadline(t, client, 500*time.Millisecond)

	versionedProposal, _, err := client.GetBeaconBlock(context.Background(), slot, []byte("test"), getTestRANDAO())
	require.NoError(t, err)
	require.NotNil(t, versionedProposal)

	actualFeeRecipient, err := versionedProposal.FeeRecipient()
	require.NoError(t, err)
	assert.Equal(t, feeRecipientAllTwos(), actualFeeRecipient,
		"MEV-optimized path should select the higher-value blinded (BN2's), not the first-arriving (BN1's)")
}

// TestGetBeaconBlock_MultiBN_MEVOptimized_DeadlinePast_FallsBackToFirstValid verifies that when the
// slot-relative soft deadline has already fired before any BN responds, the path falls through to
// waitForFirstValidProposal and returns the first valid BN response. Uses a slot in the past so the
// deadline is already past on entry.
func TestGetBeaconBlock_MultiBN_MEVOptimized_DeadlinePast_FallsBackToFirstValid(t *testing.T) {
	bn1, _ := createProposalBeaconServer(t, beaconProposalServerOptions{
		ProposalResponseDuration: 200 * time.Millisecond,
		BlindedProposal:          true,
		FeeRecipient:             feeRecipientAllOnes(),
	})
	defer bn1.Close()
	bn2, _ := createProposalBeaconServer(t, beaconProposalServerOptions{
		ProposalResponseDuration: 500 * time.Millisecond,
		BlindedProposal:          true,
		FeeRecipient:             feeRecipientAllTwos(),
	})
	defer bn2.Close()

	client := setupMultiBNClient(t, bn1.URL, bn2.URL, 1000*time.Millisecond)

	// Slot 1 is in the past (mainnet genesis is in 2020). The slot-relative deadline =
	// slotStart + 1000ms is also in the past, so softCtx is already done when collection starts.
	pastSlot := phase0.Slot(1)

	start := time.Now()
	versionedProposal, _, err := client.GetBeaconBlock(context.Background(), pastSlot, []byte("test"), getTestRANDAO())
	elapsed := time.Since(start)
	require.NoError(t, err, "fallback to first-valid should return successfully")
	require.NotNil(t, versionedProposal)

	// BN1's fee recipient confirms we returned the first valid response (BN1 at ~200ms), not the
	// slower BN2 (~500ms). Robust against timing jitter on busy CI runners.
	actualFeeRecipient, err := versionedProposal.FeeRecipient()
	require.NoError(t, err)
	assert.Equal(t, feeRecipientAllOnes(), actualFeeRecipient,
		"waitForFirstValidProposal should return BN1's response (first valid), not BN2's")
	assert.GreaterOrEqual(t, elapsed, 150*time.Millisecond,
		"should have waited for first BN response (~200ms); took %v", elapsed)
	assert.Less(t, elapsed, 450*time.Millisecond,
		"should NOT have waited for the slowest BN (~500ms); took %v", elapsed)
}

// TestGetBeaconBlock_SingleBN_MEVOptimized_WaitsUntilDeadline verifies the single-BN deadline floor:
// even though the lone BN responds quickly, GetBeaconBlock holds the block until the slot-relative
// deadline so a single-BN operator starts QBFT at the same slot time as multi-BN operators.
func TestGetBeaconBlock_SingleBN_MEVOptimized_WaitsUntilDeadline(t *testing.T) {
	bn, _ := createProposalBeaconServer(t, beaconProposalServerOptions{
		ProposalResponseDuration: 10 * time.Millisecond,
		BlindedProposal:          true,
		FeeRecipient:             feeRecipientAllOnes(),
	})
	defer bn.Close()

	client := setupSingleBNClient(t, bn.URL, true /* slotRelative */, 1500*time.Millisecond)
	const deadlineFromNow = 500 * time.Millisecond
	slot := armProposalDeadline(t, client, deadlineFromNow)

	start := time.Now()
	_, _, err := client.GetBeaconBlock(context.Background(), slot, []byte("test"), getTestRANDAO())
	elapsed := time.Since(start)
	require.NoError(t, err)

	assert.GreaterOrEqual(t, elapsed, 350*time.Millisecond,
		"single-BN MEV-optimized path should hold the block until the slot-relative deadline; took %v", elapsed)
	assert.Less(t, elapsed, 1100*time.Millisecond,
		"single-BN MEV-optimized path should return at the deadline (~500ms); took %v", elapsed)
}

// TestGetBeaconBlock_SingleBN_Legacy_NoFloor verifies that a single-BN legacy client returns as soon
// as its BN responds — the deadline floor applies only to the slot-relative (MEV-optimized) path.
func TestGetBeaconBlock_SingleBN_Legacy_NoFloor(t *testing.T) {
	bn, _ := createProposalBeaconServer(t, beaconProposalServerOptions{
		ProposalResponseDuration: 10 * time.Millisecond,
		BlindedProposal:          true,
		FeeRecipient:             feeRecipientAllOnes(),
	})
	defer bn.Close()

	client := setupSingleBNClient(t, bn.URL, false /* slotRelative */, 0)
	slot := client.getBeaconConfig().EstimatedCurrentSlot()

	start := time.Now()
	_, _, err := client.GetBeaconBlock(context.Background(), slot, []byte("test"), getTestRANDAO())
	elapsed := time.Since(start)
	require.NoError(t, err)

	assert.Less(t, elapsed, 300*time.Millisecond,
		"single-BN legacy path should return promptly, without a deadline floor; took %v", elapsed)
}

// TestGetBeaconBlock_MultiBN_LegacyPath_EarlyExitOnBlinded drives the legacy block-fetch path
// (getProposalParallelLegacy) end-to-end. Legacy early-exits on the first blinded response: with one
// fast and one slow blinded BN it must return shortly after the fast BN without waiting for the slow
// one. Legacy uses a *relative* collection timeout (unlike the MEV-optimized slot-relative
// deadline), so slot timing is irrelevant here.
func TestGetBeaconBlock_MultiBN_LegacyPath_EarlyExitOnBlinded(t *testing.T) {
	bn1, _ := createProposalBeaconServer(t, beaconProposalServerOptions{
		ProposalResponseDuration: 10 * time.Millisecond,
		BlindedProposal:          true,
		FeeRecipient:             feeRecipientAllOnes(),
	})
	defer bn1.Close()
	bn2, _ := createProposalBeaconServer(t, beaconProposalServerOptions{
		ProposalResponseDuration: 500 * time.Millisecond,
		BlindedProposal:          true,
		FeeRecipient:             feeRecipientAllTwos(),
	})
	defer bn2.Close()

	client := setupMultiBNLegacyClient(t, bn1.URL, bn2.URL, 1500*time.Millisecond)

	slot := client.getBeaconConfig().EstimatedCurrentSlot() + 2

	start := time.Now()
	versionedProposal, _, err := client.GetBeaconBlock(context.Background(), slot, []byte("test"), getTestRANDAO())
	elapsed := time.Since(start)
	require.NoError(t, err)
	require.NotNil(t, versionedProposal)

	actualFeeRecipient, err := versionedProposal.FeeRecipient()
	require.NoError(t, err)
	assert.Equal(t, feeRecipientAllOnes(), actualFeeRecipient,
		"legacy path should return the first blinded (BN1's), not wait for BN2's")
	assert.Less(t, elapsed, 350*time.Millisecond,
		"legacy path should early-exit on first blinded; took %v", elapsed)
}

// TestGetBeaconBlock_MultiBN_LegacyPath_SoftTimeoutFallsBackToFirstValid exercises the legacy path's
// *relative* collection timeout (gc.proposalSoftTimeout, measured from when the fetch starts — the
// key behavioral difference from the MEV-optimized slot-relative deadline) and its fallback. With a
// 50ms relative timeout and no BN responding that fast, the collection window expires before any
// proposal arrives, so the path falls through to waitForFirstValidProposal and returns the first
// valid response (BN1's, ~200ms).
func TestGetBeaconBlock_MultiBN_LegacyPath_SoftTimeoutFallsBackToFirstValid(t *testing.T) {
	bn1, _ := createProposalBeaconServer(t, beaconProposalServerOptions{
		ProposalResponseDuration: 200 * time.Millisecond,
		BlindedProposal:          true,
		FeeRecipient:             feeRecipientAllOnes(),
	})
	defer bn1.Close()
	bn2, _ := createProposalBeaconServer(t, beaconProposalServerOptions{
		ProposalResponseDuration: 500 * time.Millisecond,
		BlindedProposal:          true,
		FeeRecipient:             feeRecipientAllTwos(),
	})
	defer bn2.Close()

	// 50ms relative collection window — fires well before either BN responds.
	client := setupMultiBNLegacyClient(t, bn1.URL, bn2.URL, 50*time.Millisecond)

	slot := client.getBeaconConfig().EstimatedCurrentSlot() + 2

	start := time.Now()
	versionedProposal, _, err := client.GetBeaconBlock(context.Background(), slot, []byte("test"), getTestRANDAO())
	elapsed := time.Since(start)
	require.NoError(t, err)
	require.NotNil(t, versionedProposal)

	actualFeeRecipient, err := versionedProposal.FeeRecipient()
	require.NoError(t, err)
	assert.Equal(t, feeRecipientAllOnes(), actualFeeRecipient,
		"legacy fallback should return the first valid response (BN1's)")
	assert.GreaterOrEqual(t, elapsed, 150*time.Millisecond,
		"should have waited for BN1's response (~200ms); took %v", elapsed)
	assert.Less(t, elapsed, 450*time.Millisecond,
		"should NOT have waited for the slower BN2 (~500ms); took %v", elapsed)
}

// armProposalDeadline sets client.proposalSoftDeadline so the slot-relative deadline for the
// returned (current) slot lands approximately `fromNow` in the future, regardless of where "now"
// sits within the slot. This keeps floor-based collection tests fast and deterministic: the
// MEV-optimized path always waits until slot_start + proposalSoftDeadline, so a test must place
// that point a short, known time ahead. (White-box: these tests share the goclient package.)
func armProposalDeadline(t *testing.T, client *GoClient, fromNow time.Duration) phase0.Slot {
	t.Helper()
	slot := client.getBeaconConfig().EstimatedCurrentSlot()
	slotStart := client.getBeaconConfig().SlotStartTime(slot)
	client.proposalSoftDeadline = time.Since(slotStart) + fromNow
	return slot
}

// setupMultiBNClient builds a GoClient connected to two test BN servers via semicolon-separated
// URLs, on the MEV-optimized slot-relative-deadline strategy with the given deadline. Tests that
// need the deadline to land a short, known time from now should call armProposalDeadline after.
func setupMultiBNClient(t *testing.T, bn1URL, bn2URL string, deadline time.Duration) *GoClient {
	t.Helper()

	client, err := New(t.Context(), log.TestLogger(t), Options{
		BeaconNodeAddr:       bn1URL + ";" + bn2URL,
		CommonTimeout:        time.Second * 2,
		LongTimeout:          time.Second * 5,
		ProposalSoftDeadline: deadline, // positive deadline selects the MEV-optimized path
	})
	require.NoError(t, err)
	return client
}

// setupSingleBNClient builds a GoClient connected to a single test BN server. slotRelative selects
// the MEV-optimized slot-relative path (with the given deadline) vs the legacy direct fetch.
func setupSingleBNClient(t *testing.T, bnURL string, slotRelative bool, deadline time.Duration) *GoClient {
	t.Helper()

	opts := Options{
		BeaconNodeAddr: bnURL,
		CommonTimeout:  time.Second * 2,
		LongTimeout:    time.Second * 5,
	}
	if slotRelative {
		opts.ProposalSoftDeadline = deadline // positive deadline selects the MEV-optimized path
	}
	client, err := New(t.Context(), log.TestLogger(t), opts)
	require.NoError(t, err)
	return client
}

// setupMultiBNLegacyClient builds a GoClient connected to two test BN servers on the legacy
// block-fetch path, with the given relative ProposalSoftTimeout. Mirrors setupMultiBNClient (which
// covers the MEV-optimized slot-relative-deadline path).
func setupMultiBNLegacyClient(t *testing.T, bn1URL, bn2URL string, softTimeout time.Duration) *GoClient {
	t.Helper()

	client, err := New(t.Context(), log.TestLogger(t), Options{
		BeaconNodeAddr:      bn1URL + ";" + bn2URL,
		CommonTimeout:       time.Second * 2,
		LongTimeout:         time.Second * 5,
		ProposalSoftTimeout: softTimeout, // selects the legacy path (no ProposalSoftDeadline set)
	})
	require.NoError(t, err)
	return client
}

// feeRecipientAllOnes returns a fee-recipient address filled with 0x01 bytes,
// distinguishable from feeRecipientAllTwos in test assertions.
func feeRecipientAllOnes() bellatrix.ExecutionAddress {
	var addr bellatrix.ExecutionAddress
	for i := range addr {
		addr[i] = 1
	}
	return addr
}

// feeRecipientAllTwos returns a fee-recipient address filled with 0x02 bytes.
func feeRecipientAllTwos() bellatrix.ExecutionAddress {
	var addr bellatrix.ExecutionAddress
	for i := range addr {
		addr[i] = 2
	}
	return addr
}
