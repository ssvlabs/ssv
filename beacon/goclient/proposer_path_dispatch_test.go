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

// Tests for the block-fetch path dispatch (BlockFetchPathSafe / Legacy / MEVOptimized).
// See docs/MEV_CONSIDERATIONS.md for path semantics.

// TestNew_StoresBlockFetchPath verifies that the selected path and its associated
// timing field (proposalSoftDeadline / proposalSoftTimeout) get propagated from
// Options into the resulting GoClient.
func TestNew_StoresBlockFetchPath(t *testing.T) {
	for _, path := range []BlockFetchPath{BlockFetchPathSafe, BlockFetchPathLegacy, BlockFetchPathMEVOptimized} {
		t.Run(path.String(), func(t *testing.T) {
			server, _ := createProposalBeaconServer(t, beaconProposalServerOptions{})
			defer server.Close()

			// In production these resolved values come from cli/operator config resolution;
			// here we set them explicitly and verify New propagates them onto the GoClient.
			base := Options{
				BeaconNodeAddr: server.URL,
				CommonTimeout:  time.Second * 2,
				LongTimeout:    time.Second * 5,
				BlockFetchPath: path,
			}
			switch path {
			case BlockFetchPathSafe, BlockFetchPathMEVOptimized:
				base.ProposalSoftDeadline = 1100 * time.Millisecond
			case BlockFetchPathLegacy:
				base.ProposalSoftTimeout = 1800 * time.Millisecond
			}

			client, err := New(t.Context(), log.TestLogger(t), base)
			require.NoError(t, err)

			assert.Equal(t, path, client.blockFetchPath, "GoClient.blockFetchPath should reflect opt.BlockFetchPath")

			switch path {
			case BlockFetchPathSafe, BlockFetchPathMEVOptimized:
				assert.Equal(t, 1100*time.Millisecond, client.proposalSoftDeadline,
					"New should propagate ProposalSoftDeadline")
			case BlockFetchPathLegacy:
				assert.Equal(t, 1800*time.Millisecond, client.proposalSoftTimeout,
					"New should propagate ProposalSoftTimeout")
			}
		})
	}
}

// TestGetBeaconBlock_MultiBN_SafePath_EarlyExitOnBlinded verifies the safe path's
// early-exit-on-first-blinded behavior. With one fast and one slow BN both returning
// blinded proposals, the safe path should return quickly after the fast BN responds,
// without waiting for the slow one.
func TestGetBeaconBlock_MultiBN_SafePath_EarlyExitOnBlinded(t *testing.T) {
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

	client := setupMultiBNClient(t, bn1.URL, bn2.URL, BlockFetchPathSafe, 1500*time.Millisecond)

	// Use a slot starting in the near future so the slot-relative deadline lands
	// well after both BN responses (we want to observe the early-exit on blinded,
	// not the deadline firing).
	slot := client.getBeaconConfig().EstimatedCurrentSlot() + 2

	start := time.Now()
	_, _, err := client.GetBeaconBlock(context.Background(), slot, []byte("test"), getTestRANDAO())
	elapsed := time.Since(start)
	require.NoError(t, err)

	// Safe path should early-exit on BN1's blinded response (~10ms) and NOT wait for
	// BN2 (~500ms). The 350ms ceiling sits well below BN2's response time while
	// tolerating HTTP / goroutine / loaded-CI overhead.
	assert.Less(t, elapsed, 350*time.Millisecond,
		"safe path should early-exit on first blinded; took %v", elapsed)
}

// TestGetBeaconBlock_MultiBN_MEVOptimizedPath_NoEarlyExit verifies that the MEV-optimized
// path does NOT early-exit on the first blinded response — it keeps collecting until all
// BNs respond (or the soft deadline fires). With the same setup as the safe-path test,
// the MEV-optimized path should wait for the slow BN.
func TestGetBeaconBlock_MultiBN_MEVOptimizedPath_NoEarlyExit(t *testing.T) {
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

	client := setupMultiBNClient(t, bn1.URL, bn2.URL, BlockFetchPathMEVOptimized, 1500*time.Millisecond)

	slot := client.getBeaconConfig().EstimatedCurrentSlot() + 2

	start := time.Now()
	_, _, err := client.GetBeaconBlock(context.Background(), slot, []byte("test"), getTestRANDAO())
	elapsed := time.Since(start)
	require.NoError(t, err)

	// MEV-optimized path should NOT early-exit; it waits for BN2's response at ~500ms
	// before returning the best-scored proposal. The 400ms floor tolerates clock jitter.
	assert.GreaterOrEqual(t, elapsed, 400*time.Millisecond,
		"MEV-optimized path should wait for the slower BN; took %v", elapsed)
}

// TestGetBeaconBlock_MultiBN_MEVOptimizedPath_HighestScoringBlindedWins verifies that
// when multiple BNs return blinded proposals within the collection window, the
// MEV-optimized path selects the one with the highest scoreProposal value (sum of
// ConsensusValue and ExecutionValue) rather than the first-arriving one. BN1 returns
// a fast low-value blinded; BN2 returns a slow high-value blinded — the function must
// return BN2's proposal.
func TestGetBeaconBlock_MultiBN_MEVOptimizedPath_HighestScoringBlindedWins(t *testing.T) {
	bn1, _ := createProposalBeaconServer(t, beaconProposalServerOptions{
		ProposalResponseDuration: 10 * time.Millisecond,
		BlindedProposal:          true,
		FeeRecipient:             feeRecipientAllOnes(),
		ExecutionValue:           big.NewInt(1_000_000), // low bid
	})
	defer bn1.Close()
	bn2, _ := createProposalBeaconServer(t, beaconProposalServerOptions{
		ProposalResponseDuration: 300 * time.Millisecond,
		BlindedProposal:          true,
		FeeRecipient:             feeRecipientAllTwos(),
		ExecutionValue:           big.NewInt(5_000_000), // high bid (must win)
	})
	defer bn2.Close()

	client := setupMultiBNClient(t, bn1.URL, bn2.URL, BlockFetchPathMEVOptimized, 1500*time.Millisecond)

	slot := client.getBeaconConfig().EstimatedCurrentSlot() + 2

	versionedProposal, _, err := client.GetBeaconBlock(context.Background(), slot, []byte("test"), getTestRANDAO())
	require.NoError(t, err)
	require.NotNil(t, versionedProposal)

	actualFeeRecipient, err := versionedProposal.FeeRecipient()
	require.NoError(t, err)
	assert.Equal(t, feeRecipientAllTwos(), actualFeeRecipient,
		"MEV-optimized path should select the higher-value blinded (BN2's), not the first-arriving (BN1's)")
}

// TestGetBeaconBlock_MultiBN_SoftDeadlineFires_FallsBackToFirstValid verifies that
// when the slot-relative soft deadline has already fired before any BN responds,
// the parallel-fetch path falls through to waitForFirstValidProposal and returns
// the first valid BN response. Uses a slot in the past so the deadline is past.
func TestGetBeaconBlock_MultiBN_SoftDeadlineFires_FallsBackToFirstValid(t *testing.T) {
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

	client := setupMultiBNClient(t, bn1.URL, bn2.URL, BlockFetchPathSafe, 1000*time.Millisecond)

	// Slot 1 is in the past (mainnet genesis is in 2020). The slot-relative
	// deadline = slotStart + 1000ms is also in the past, so softCtx is already
	// done when the collection loop starts.
	pastSlot := phase0.Slot(1)

	start := time.Now()
	versionedProposal, _, err := client.GetBeaconBlock(context.Background(), pastSlot, []byte("test"), getTestRANDAO())
	elapsed := time.Since(start)
	require.NoError(t, err, "fallback to first-valid should return successfully")
	require.NotNil(t, versionedProposal)

	// Primary assertion: BN1's fee recipient confirms we returned with the first
	// valid response (BN1 at ~200ms), not the slower BN2 (~500ms). This is robust
	// against timing jitter on busy CI runners.
	actualFeeRecipient, err := versionedProposal.FeeRecipient()
	require.NoError(t, err)
	assert.Equal(t, feeRecipientAllOnes(), actualFeeRecipient,
		"waitForFirstValidProposal should return BN1's response (first valid), not BN2's")

	// Sanity check on elapsed: must be at least BN1's response time, and the upper
	// bound just confirms we didn't end up waiting for BN2. Margins kept generous
	// for CI scheduling overhead.
	assert.GreaterOrEqual(t, elapsed, 150*time.Millisecond,
		"should have waited for first BN response (~200ms); took %v", elapsed)
	assert.Less(t, elapsed, 450*time.Millisecond,
		"should NOT have waited for the slowest BN (~500ms); took %v", elapsed)
}

// setupMultiBNClient builds a GoClient connected to two test BN servers via
// semicolon-separated URLs, with the given block-fetch path and deadline. Used by
// the per-path behavior tests.
func setupMultiBNClient(t *testing.T, bn1URL, bn2URL string, path BlockFetchPath, deadline time.Duration) *GoClient {
	t.Helper()

	client, err := New(t.Context(), log.TestLogger(t), Options{
		BeaconNodeAddr:       bn1URL + ";" + bn2URL,
		CommonTimeout:        time.Second * 2,
		LongTimeout:          time.Second * 5,
		ProposalSoftDeadline: deadline,
		BlockFetchPath:       path,
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
