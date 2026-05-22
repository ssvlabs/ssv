package ssv

import (
	"testing"

	"github.com/attestantio/go-eth2-client/spec/phase0"
	spectypes "github.com/ssvlabs/ssv-spec/types"
	"github.com/stretchr/testify/require"
)

// TestVoteCheckerSourceTargetEpoch pins the behaviour of the source/target epoch check at
// value_check.go:47.
//
// The current implementation uses `source.Epoch >= target.Epoch` to reject the BeaconVote,
// which is stricter than the Ethereum consensus-specs validity rule for attestations. Per
// `process_attestation` in https://github.com/ethereum/consensus-specs/blob/master/specs/phase0/beacon-chain.md
// the only attestation-data validity constraints are:
//   - data.target.epoch ∈ {get_previous_epoch(state), get_current_epoch(state)}
//   - data.source == state.{current,previous}_justified_checkpoint (per target.epoch)
//
// There is no `source.epoch < target.epoch` rule at validity time; that inequality is the
// Casper FFG surround-vote *slashing* rule (consensus-specs phase0/beacon-chain.md, the
// "Attester Slashing" section), and SSV's slashing protection covers it independently a few
// lines below.
//
// The over-strict `>=` rejects the genesis-epoch case where both source and target legitimately
// equal 0 (because state.{current,previous}_justified_checkpoint == Checkpoint(0, ⊥) at genesis).
// This is the root cause of the cross-client interop failure documented in
// docs/v2.4.3-QA-verification-plan.md §B1 — every fresh-genesis cross-client test fails on
// every committee duty throughout epoch 0.
//
// The corresponding ssv-spec rule with the same bug is at ssv-spec v1.2.2
// ssv/value_check.go:54. Both should be fixed together.
//
// When the fix lands (changing `>=` to `>`, or removing the check entirely), the genesis
// sub-test's assertion needs to flip: the source/target gate should no longer fire on
// `(0, 0)`. The source-greater-than-target sub-test is unaffected by the fix.
func TestVoteCheckerSourceTargetEpoch(t *testing.T) {
	t.Run("genesis: source.Epoch == target.Epoch == 0 currently rejected (B1 bug)", func(t *testing.T) {
		checker := NewVoteChecker(nil, 0, []phase0.BLSPubKey{}, &spectypes.BeaconVote{})
		err := checker.CheckValue(encodeBeaconVote(t, 0, 0))
		require.ErrorContains(t, err, "source >= target")
		// TODO(B1 fix): once the fix lands, this assertion should flip — at minimum
		// the source/target gate should no longer fire on (0, 0).
	})

	t.Run("source.Epoch > target.Epoch is rejected (unchanged by the B1 fix)", func(t *testing.T) {
		checker := NewVoteChecker(nil, 0, []phase0.BLSPubKey{}, &spectypes.BeaconVote{})
		err := checker.CheckValue(encodeBeaconVote(t, 1, 0))
		require.ErrorContains(t, err, "source >= target")
	})
}

// encodeBeaconVote returns SSZ bytes for a BeaconVote with the requested source/target epochs
// and zero block root. Suitable for tests that exercise the source/target gate, which fires
// before any downstream check (slashing or expected-vote) so a nil signer/expectedVote on the
// voteChecker is fine.
func encodeBeaconVote(t *testing.T, sourceEpoch, targetEpoch phase0.Epoch) []byte {
	t.Helper()
	bv := &spectypes.BeaconVote{
		BlockRoot: phase0.Root{},
		Source:    &phase0.Checkpoint{Epoch: sourceEpoch},
		Target:    &phase0.Checkpoint{Epoch: targetEpoch},
	}
	data, err := bv.Encode()
	require.NoError(t, err)
	return data
}
