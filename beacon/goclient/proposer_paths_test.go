package goclient

import (
	"context"
	"testing"
	"time"

	"github.com/attestantio/go-eth2-client/spec/bellatrix"
	"github.com/attestantio/go-eth2-client/spec/phase0"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ssvlabs/ssv/observability/log"
)

// Tests for the block-fetch path dispatch (BlockFetchPathSafe / Legacy / MEVOptimized).
// See docs/MEV_CONSIDERATIONS.md for the three-path model.

// TestNew_StoresBlockFetchPath verifies that the selected path and its associated
// timing field (proposalSoftDeadline / proposalSoftTimeout) get propagated from
// Options into the resulting GoClient.
func TestNew_StoresBlockFetchPath(t *testing.T) {
	for _, path := range []BlockFetchPath{BlockFetchPathSafe, BlockFetchPathLegacy, BlockFetchPathMEVOptimized} {
		t.Run(path.String(), func(t *testing.T) {
			server, _ := createProposalBeaconServer(t, beaconProposalServerOptions{})
			defer server.Close()

			base := Options{
				BeaconNodeAddr: server.URL,
				CommonTimeout:  time.Second * 2,
				LongTimeout:    time.Second * 5,
			}
			if path == BlockFetchPathMEVOptimized {
				base.ProposalSoftDeadline = 1100 * time.Millisecond
			}
			opt, err := NewOptions(base, 0, path)
			require.NoError(t, err)

			client, err := New(t.Context(), log.TestLogger(t), opt)
			require.NoError(t, err)

			assert.Equal(t, path, client.blockFetchPath, "GoClient.blockFetchPath should reflect opt.BlockFetchPath")

			switch path {
			case BlockFetchPathSafe:
				assert.Equal(t, DefaultProposalSoftDeadline, client.proposalSoftDeadline,
					"safe path should default proposalSoftDeadline to %v", DefaultProposalSoftDeadline)
			case BlockFetchPathMEVOptimized:
				assert.Equal(t, 1100*time.Millisecond, client.proposalSoftDeadline,
					"MEV-optimized path should propagate operator-set ProposalSoftDeadline")
			case BlockFetchPathLegacy:
				assert.NotZero(t, client.proposalSoftTimeout,
					"legacy path should have non-zero proposalSoftTimeout")
			}
		})
	}
}

// TestGetBeaconBlock_MultiBN_Path1_EarlyExitOnBlinded verifies the safe path's
// early-exit-on-first-blinded behavior. With one fast and one slow BN both returning
// blinded proposals, the safe path should return quickly after the fast BN responds,
// without waiting for the slow one.
func TestGetBeaconBlock_MultiBN_Path1_EarlyExitOnBlinded(t *testing.T) {
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

	// Path 1 should early-exit on BN1's blinded response (~10ms) and NOT wait for
	// BN2 (~500ms). A generous 250ms ceiling tolerates HTTP / goroutine overhead.
	assert.Less(t, elapsed, 250*time.Millisecond,
		"Path 1 should early-exit on first blinded; took %v", elapsed)
}

// TestGetBeaconBlock_MultiBN_Path2_NoEarlyExit verifies that the MEV-optimized
// path does NOT early-exit on the first blinded response — it keeps collecting
// until all BNs respond (or the soft deadline fires). With the same setup as the
// safe-path test, path 2 should wait for the slow BN.
func TestGetBeaconBlock_MultiBN_Path2_NoEarlyExit(t *testing.T) {
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

	// Path 2 should NOT early-exit; it waits for BN2's response at ~500ms before
	// returning the best-scored proposal. The 400ms floor tolerates clock jitter.
	assert.GreaterOrEqual(t, elapsed, 400*time.Millisecond,
		"Path 2 should wait for the slower BN; took %v", elapsed)
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
	_, _, err := client.GetBeaconBlock(context.Background(), pastSlot, []byte("test"), getTestRANDAO())
	elapsed := time.Since(start)
	require.NoError(t, err, "fallback to first-valid should return successfully")

	// Should return after BN1 responds (~200ms), not wait for BN2 (~500ms). This
	// confirms waitForFirstValidProposal is invoked (returning the first valid
	// response, bounded by the parent context's slot deadline).
	assert.GreaterOrEqual(t, elapsed, 150*time.Millisecond,
		"should have waited for first BN response (~200ms); took %v", elapsed)
	assert.Less(t, elapsed, 400*time.Millisecond,
		"should NOT have waited for the slowest BN (~500ms); took %v", elapsed)
}

// setupMultiBNClient builds a GoClient connected to two test BN servers via
// semicolon-separated URLs, with the given block-fetch path and deadline. Used by
// the per-path behavior tests.
func setupMultiBNClient(t *testing.T, bn1URL, bn2URL string, path BlockFetchPath, deadline time.Duration) *GoClient {
	t.Helper()

	base := Options{
		BeaconNodeAddr:       bn1URL + ";" + bn2URL,
		CommonTimeout:        time.Second * 2,
		LongTimeout:          time.Second * 5,
		ProposalSoftDeadline: deadline,
	}
	opt, err := NewOptions(base, 0, path)
	require.NoError(t, err)

	client, err := New(t.Context(), log.TestLogger(t), opt)
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
