package goclient

import (
	"context"
	"math/big"
	"testing"
	"time"

	"github.com/attestantio/go-eth2-client/api"
	"github.com/attestantio/go-eth2-client/spec/bellatrix"
	"github.com/attestantio/go-eth2-client/spec/phase0"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ssvlabs/ssv/observability/log"
)

// Tests for the block-fetch path dispatch (BlockFetchPathSafe / Legacy / MEVOptimized).
// See docs/MEV_CONSIDERATIONS.md for path semantics.
//
// These tests gate BN responses on Release channels rather than time.Sleep delays,
// so the assertions are identity/ordering-based rather than wall-clock-based.
// A 2s safety timeout on the result channel catches the "GetBeaconBlock never returns"
// failure mode without depending on tight CI-sensitive elapsed-time bounds.

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

// TestGetBeaconBlock_MultiBN_SafePath_EarlyExitOnBlinded verifies the safe path's
// early-exit-on-first-blinded behavior. With BN1 released and BN2 left blocked,
// the safe path must return on BN1's blinded response without waiting for BN2.
func TestGetBeaconBlock_MultiBN_SafePath_EarlyExitOnBlinded(t *testing.T) {
	release1 := make(chan struct{})
	release2 := make(chan struct{}) // intentionally never closed

	bn1, _ := createProposalBeaconServer(t, beaconProposalServerOptions{
		Release:         release1,
		BlindedProposal: true,
		FeeRecipient:    feeRecipientAllOnes(),
	})
	defer bn1.Close()
	bn2, _ := createProposalBeaconServer(t, beaconProposalServerOptions{
		Release:         release2,
		BlindedProposal: true,
		FeeRecipient:    feeRecipientAllTwos(),
	})
	defer bn2.Close()

	client := setupMultiBNClient(t, bn1.URL, bn2.URL, BlockFetchPathSafe, 1500*time.Millisecond)

	// Future slot so the slot-relative deadline lands after both BN responses —
	// we want to observe early-exit, not the deadline firing.
	slot := client.getBeaconConfig().EstimatedCurrentSlot() + 2

	resultCh, cancel := launchGetBeaconBlock(t, client, slot)
	defer cancel() // unblocks BN2's still-pending request when test returns

	close(release1)

	proposal := requireBeaconBlockResult(t, resultCh, "safe path should early-exit on first blinded")
	assertProposalFeeRecipient(t, proposal, feeRecipientAllOnes(),
		"safe path should return BN1's blinded (early-exit), not BN2's")
}

// TestGetBeaconBlock_MultiBN_MEVOptimizedPath_NoEarlyExit verifies that the
// MEV-optimized path does NOT early-exit on the first blinded response. After
// releasing BN1, GetBeaconBlock must still be waiting; only after releasing BN2
// does it return.
func TestGetBeaconBlock_MultiBN_MEVOptimizedPath_NoEarlyExit(t *testing.T) {
	release1 := make(chan struct{})
	release2 := make(chan struct{})

	bn1, _ := createProposalBeaconServer(t, beaconProposalServerOptions{
		Release:         release1,
		BlindedProposal: true,
		FeeRecipient:    feeRecipientAllOnes(),
	})
	defer bn1.Close()
	bn2, _ := createProposalBeaconServer(t, beaconProposalServerOptions{
		Release:         release2,
		BlindedProposal: true,
		FeeRecipient:    feeRecipientAllTwos(),
	})
	defer bn2.Close()

	client := setupMultiBNClient(t, bn1.URL, bn2.URL, BlockFetchPathMEVOptimized, 1500*time.Millisecond)

	slot := client.getBeaconConfig().EstimatedCurrentSlot() + 2

	resultCh, cancel := launchGetBeaconBlock(t, client, slot)
	defer cancel()

	close(release1)

	// MEV-optimized path keeps collecting after blinded. A short non-return check
	// (100ms) is enough to detect a regression to early-exit behavior; we're
	// asserting "no event for a brief window" rather than the much weaker
	// "wall-clock bound around 500ms".
	assertNoReturnWithin(t, resultCh, 100*time.Millisecond,
		"MEV-optimized path returned after BN1 alone — should not early-exit on blinded")

	close(release2)

	requireBeaconBlockResult(t, resultCh, "MEV-optimized path should return after both BNs respond")
}

// TestGetBeaconBlock_MultiBN_MEVOptimizedPath_HighestScoringBlindedWins verifies
// that when multiple BNs return blinded proposals within the collection window,
// the MEV-optimized path selects the one with the highest scoreProposal value
// (sum of ConsensusValue and ExecutionValue) rather than the first-arriving one.
func TestGetBeaconBlock_MultiBN_MEVOptimizedPath_HighestScoringBlindedWins(t *testing.T) {
	release1 := make(chan struct{})
	release2 := make(chan struct{})

	bn1, _ := createProposalBeaconServer(t, beaconProposalServerOptions{
		Release:         release1,
		BlindedProposal: true,
		FeeRecipient:    feeRecipientAllOnes(),
		ExecutionValue:  big.NewInt(1_000_000), // low bid
	})
	defer bn1.Close()
	bn2, _ := createProposalBeaconServer(t, beaconProposalServerOptions{
		Release:         release2,
		BlindedProposal: true,
		FeeRecipient:    feeRecipientAllTwos(),
		ExecutionValue:  big.NewInt(5_000_000), // high bid (must win)
	})
	defer bn2.Close()

	client := setupMultiBNClient(t, bn1.URL, bn2.URL, BlockFetchPathMEVOptimized, 1500*time.Millisecond)

	slot := client.getBeaconConfig().EstimatedCurrentSlot() + 2

	resultCh, cancel := launchGetBeaconBlock(t, client, slot)
	defer cancel()

	// Release BN1 first so it arrives first (lower bid). MEV-optimized must keep
	// collecting until BN2 (higher bid) arrives, then prefer BN2 by score.
	close(release1)
	close(release2)

	proposal := requireBeaconBlockResult(t, resultCh, "MEV-optimized path should select highest-scoring proposal")
	assertProposalFeeRecipient(t, proposal, feeRecipientAllTwos(),
		"MEV-optimized path should select the higher-value blinded (BN2's), not the first-arriving (BN1's)")
}

// TestGetBeaconBlock_MultiBN_SoftDeadlineFires_FallsBackToFirstValid verifies that
// when the slot-relative soft deadline has already fired before any BN responds,
// the parallel-fetch path falls through to waitForFirstValidProposal and returns
// the first valid BN response. Uses a slot in the past so the deadline is past.
func TestGetBeaconBlock_MultiBN_SoftDeadlineFires_FallsBackToFirstValid(t *testing.T) {
	release1 := make(chan struct{})
	release2 := make(chan struct{}) // intentionally never closed

	bn1, _ := createProposalBeaconServer(t, beaconProposalServerOptions{
		Release:         release1,
		BlindedProposal: true,
		FeeRecipient:    feeRecipientAllOnes(),
	})
	defer bn1.Close()
	bn2, _ := createProposalBeaconServer(t, beaconProposalServerOptions{
		Release:         release2,
		BlindedProposal: true,
		FeeRecipient:    feeRecipientAllTwos(),
	})
	defer bn2.Close()

	client := setupMultiBNClient(t, bn1.URL, bn2.URL, BlockFetchPathSafe, 1000*time.Millisecond)

	// Slot 1 is in the past (mainnet genesis is in 2020). The slot-relative
	// deadline = slotStart + 1000ms is also in the past, so softCtx is already
	// done when the collection loop starts.
	pastSlot := phase0.Slot(1)

	resultCh, cancel := launchGetBeaconBlock(t, client, pastSlot)
	defer cancel()

	close(release1)

	proposal := requireBeaconBlockResult(t, resultCh, "waitForFirstValidProposal should return the first BN response")
	assertProposalFeeRecipient(t, proposal, feeRecipientAllOnes(),
		"waitForFirstValidProposal should return BN1's response (first released), not BN2's")
}

// beaconBlockResult bundles the values returned by client.GetBeaconBlock for
// transmission over a channel from the background goroutine spawned by
// launchGetBeaconBlock.
type beaconBlockResult struct {
	proposal *api.VersionedProposal
	err      error
}

// launchGetBeaconBlock runs client.GetBeaconBlock in a background goroutine and
// returns a channel that receives the result when it returns, plus a cancel
// function that aborts any in-flight BN requests (used to unblock test-server
// handlers whose Release channels weren't closed).
func launchGetBeaconBlock(t *testing.T, client *GoClient, slot phase0.Slot) (<-chan beaconBlockResult, context.CancelFunc) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	resultCh := make(chan beaconBlockResult, 1)
	go func() {
		p, _, e := client.GetBeaconBlock(ctx, slot, []byte("test"), getTestRANDAO())
		resultCh <- beaconBlockResult{proposal: p, err: e}
	}()
	return resultCh, cancel
}

// requireBeaconBlockResult waits for the result channel with a 2s safety timeout,
// failing the test if no result arrives. Returns the proposal on success.
func requireBeaconBlockResult(t *testing.T, resultCh <-chan beaconBlockResult, msg string) *api.VersionedProposal {
	t.Helper()
	select {
	case r := <-resultCh:
		require.NoError(t, r.err, msg)
		require.NotNil(t, r.proposal, msg)
		return r.proposal
	case <-time.After(2 * time.Second):
		t.Fatalf("GetBeaconBlock did not return within 2s: %s", msg)
		return nil
	}
}

// assertNoReturnWithin fails the test if a result arrives on the channel within
// the given window. Used to verify that the MEV-optimized path does NOT
// early-exit after one BN responds.
func assertNoReturnWithin(t *testing.T, resultCh <-chan beaconBlockResult, window time.Duration, msg string) {
	t.Helper()
	select {
	case <-resultCh:
		t.Fatalf("unexpected early return within %v: %s", window, msg)
	case <-time.After(window):
		// Expected: still waiting.
	}
}

// assertProposalFeeRecipient extracts the fee recipient from a VersionedProposal
// and asserts equality with the expected value.
func assertProposalFeeRecipient(t *testing.T, proposal *api.VersionedProposal, expected bellatrix.ExecutionAddress, msg string) {
	t.Helper()
	actual, err := proposal.FeeRecipient()
	require.NoError(t, err)
	assert.Equal(t, expected, actual, msg)
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
