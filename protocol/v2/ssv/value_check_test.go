package ssv

import (
	"testing"

	"github.com/attestantio/go-eth2-client/spec/phase0"
	spectypes "github.com/ssvlabs/ssv-spec/types"
	"github.com/stretchr/testify/require"
)

// TestVoteCheckerSourceTargetEpoch pins the behavior of the source/target epoch check at
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
// This causes every fresh-genesis cross-client test to fail on every committee duty throughout
// epoch 0 — observed in the v2.4.3 ↔ Anchor v1.2.3 interop logs.
//
// The corresponding ssv-spec rule with the same bug is at ssv-spec v1.2.2 ssv/value_check.go:53;
// filed upstream at ssvlabs/ssv-spec#631. SSV-Go's one-character mirror fix (`>=` → `>`) is held
// back until spec-maintainer direction is confirmed on #631, to avoid SSV drifting from the
// canonical spec unilaterally.
//
// When the spec fix lands, the genesis sub-test's assertion needs to flip: the source/target
// gate should no longer fire on `(0, 0)`. The source-greater-than-target sub-test is unaffected
// by the fix.
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

// TestValidateNoDuplicateAggregatorCommittee covers the duplicate-rejection that closes the
// per-index partial-signature cap gap: a validator index may repeat across the two sets (an
// aggregator and a contributor for the same numeric index are distinct), but not within a set.
func TestValidateNoDuplicateAggregatorCommittee(t *testing.T) {
	agg := func(vi phase0.ValidatorIndex, ci uint64) spectypes.AssignedAggregator {
		return spectypes.AssignedAggregator{ValidatorIndex: vi, CommitteeIndex: ci}
	}

	t.Run("clean data passes", func(t *testing.T) {
		cd := &spectypes.AggregatorCommitteeConsensusData{
			Aggregators:  []spectypes.AssignedAggregator{agg(1, 0), agg(2, 0), agg(1, 3)},
			Contributors: []spectypes.AssignedAggregator{agg(1, 0), agg(1, 1), agg(2, 0)},
		}
		require.NoError(t, validateNoDuplicateAggregatorCommittee(cd))
	})

	t.Run("same (validator, index) across the two sets is allowed", func(t *testing.T) {
		cd := &spectypes.AggregatorCommitteeConsensusData{
			Aggregators:  []spectypes.AssignedAggregator{agg(7, 0)},
			Contributors: []spectypes.AssignedAggregator{agg(7, 0)},
		}
		require.NoError(t, validateNoDuplicateAggregatorCommittee(cd))
	})

	t.Run("duplicate aggregator rejected", func(t *testing.T) {
		cd := &spectypes.AggregatorCommitteeConsensusData{
			Aggregators: []spectypes.AssignedAggregator{agg(5, 2), agg(5, 2)},
		}
		require.ErrorContains(t, validateNoDuplicateAggregatorCommittee(cd), "duplicate aggregator")
	})

	t.Run("duplicate contributor rejected", func(t *testing.T) {
		cd := &spectypes.AggregatorCommitteeConsensusData{
			Contributors: []spectypes.AssignedAggregator{agg(9, 1), agg(9, 1)},
		}
		require.ErrorContains(t, validateNoDuplicateAggregatorCommittee(cd), "duplicate contributor")
	})
}
