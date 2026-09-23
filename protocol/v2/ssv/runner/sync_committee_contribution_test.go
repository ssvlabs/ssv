package runner

import (
	"context"
	"errors"
	"slices"
	"testing"

	"github.com/attestantio/go-eth2-client/spec"
	"github.com/attestantio/go-eth2-client/spec/altair"
	"github.com/attestantio/go-eth2-client/spec/phase0"
	spectypes "github.com/ssvlabs/ssv-spec/types"
	spectestingutils "github.com/ssvlabs/ssv-spec/types/testingutils"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	"github.com/ssvlabs/ssv/protocol/v2/blockchain/beacon"
	protocoltesting "github.com/ssvlabs/ssv/protocol/v2/testing"
	ssvtypes "github.com/ssvlabs/ssv/protocol/v2/types"
)

// TestSortBySubnet pins the deterministic, cross-node canonical ordering that
// sortBySubnet provides for sync-committee contributions. Two nodes building
// contributions from the same logical set must agree on the resulting SSZ root,
// which requires an identical (subnet-ascending) order before
// GetSyncCommitteeContribution — otherwise the contribution roots diverge and the
// duty silently fails. This is a standalone unit test with no dependency on the
// runner test-kit (per PR #2899 review); the broader end-to-end regression test
// lands with the rest of upstream #2859.
func TestSortBySubnet(t *testing.T) {
	// mk builds a pair whose proof encodes its subnet in the first byte, so we can
	// assert the (subnet, proof) pairing stays intact across the sort.
	mk := func(subnet uint64) subnetSelectionProof {
		var p phase0.BLSSignature
		p[0] = byte(subnet)
		return subnetSelectionProof{subnet: subnet, selectionProof: p}
	}

	t.Run("orders ascending and preserves pairing", func(t *testing.T) {
		pairs := []subnetSelectionProof{mk(7), mk(0), mk(3), mk(12), mk(1)}

		sortBySubnet(pairs)

		got := make([]uint64, len(pairs))
		for i, p := range pairs {
			got[i] = p.subnet
			require.Equal(t, byte(p.subnet), p.selectionProof[0], "proof must stay paired with its subnet")
		}
		require.Equal(t, []uint64{0, 1, 3, 7, 12}, got)
	})

	t.Run("already sorted stays in order", func(t *testing.T) {
		pairs := []subnetSelectionProof{mk(0), mk(1), mk(2)}
		sortBySubnet(pairs)
		require.Equal(t, []uint64{0, 1, 2}, []uint64{pairs[0].subnet, pairs[1].subnet, pairs[2].subnet})
	})

	t.Run("empty and nil do not panic", func(t *testing.T) {
		require.NotPanics(t, func() { sortBySubnet(nil) })
		require.NotPanics(t, func() { sortBySubnet([]subnetSelectionProof{}) })
	})
}

// syncCommitteeContributionSubmitCaptureBeacon embeds the shared testing beacon node and captures the subnet
// of each submitted contribution, rejecting those of rejectedSubnets.
type syncCommitteeContributionSubmitCaptureBeacon struct {
	beacon.BeaconNode

	rejectedSubnets  []uint64
	submittedSubnets []uint64
}

func (b *syncCommitteeContributionSubmitCaptureBeacon) SubmitSignedContributionAndProof(_ context.Context, contribution *altair.SignedContributionAndProof) error {
	subnet := contribution.Message.Contribution.SubcommitteeIndex
	if slices.Contains(b.rejectedSubnets, subnet) {
		return errors.New("contribution rejected")
	}
	b.submittedSubnets = append(b.submittedSubnets, subnet)
	return nil
}

// decideSyncCommitteeContributions starts a duty on runner and decides the three contributions the
// post-consensus fixtures sign, skipping pre-consensus.
func decideSyncCommitteeContributions(t *testing.T, runner *SyncCommitteeAggregatorRunner, keySet *spectestingutils.TestKeySet) {
	t.Helper()

	ctx, logger := t.Context(), zap.NewNop()
	duty := &spectypes.ValidatorDuty{
		Type:                          spectypes.BNRoleSyncCommitteeContribution,
		PubKey:                        spectestingutils.TestingValidatorPubKey,
		Slot:                          spectestingutils.TestingDutySlot,
		ValidatorIndex:                spectestingutils.TestingValidatorIndex,
		ValidatorSyncCommitteeIndices: spectestingutils.TestingContributionProofIndexes,
	}
	require.NoError(t, runner.StartNewDuty(ctx, logger, duty, keySet.Threshold))

	consensusData := &spectypes.ProposerConsensusData{
		Duty:    *duty,
		Version: spec.DataVersionAltair,
		DataSSZ: spectestingutils.TestingContributionsDataBytes,
	}
	require.NoError(t, runner.decide(ctx, logger, duty.Slot, consensusData, runner.ValCheck))
	for _, msg := range spectestingutils.SSVDecidingMsgsV(consensusData, keySet, ssvtypes.RoleSyncCommitteeContribution) {
		require.NoError(t, runner.ProcessConsensus(ctx, logger, msg))
	}
}

// Each root's contribution is submitted once the root reconstructs. A bad share in one root's quorum doesn't hold
// the other contributions back, and doesn't fail the duty: the fallback drops it, and an honest share bringing
// the root back to quorum submits its contribution and completes the duty.
func TestSyncCommitteeAggregatorProcessPostConsensusSubmitsEachRoot(t *testing.T) {
	t.Parallel()

	ctx, logger := t.Context(), zap.NewNop()
	testBeacon := &syncCommitteeContributionSubmitCaptureBeacon{BeaconNode: protocoltesting.NewTestingBeaconNodeWrapped()}
	runner, keySet := newSyncCommitteeAggregatorRunnerForTest(t, testBeacon)
	decideSyncCommitteeContributions(t, runner, keySet)
	concluded := observeDutyConclusion(runner.BaseRunner)
	msg := func(op spectypes.OperatorID) *spectypes.PartialSignatureMessages {
		return spectestingutils.PostConsensusSyncCommitteeContributionMsg(keySet.Shares[op], op, keySet)
	}

	// Operator 1's share for subnet 2's contribution carries operator 2's signature, so it fails verification.
	bad := msg(1)
	bad.Messages[2].PartialSignature = msg(2).Messages[2].PartialSignature
	require.NoError(t, runner.ProcessPostConsensus(ctx, logger, bad))
	require.NoError(t, runner.ProcessPostConsensus(ctx, logger, msg(2)))
	err := runner.ProcessPostConsensus(ctx, logger, msg(3))
	require.ErrorContains(t, err, "invalid signatures")
	require.True(t, isRecoverableReconstructError(err))
	requireSpecCode(t, err, spectypes.PostConsensusQuorumWithInvalidSignatures)
	require.ElementsMatch(t, []uint64{0, 1}, testBeacon.submittedSubnets, "subnet 2 doesn't hold back the others")
	require.Empty(t, concluded, "the failed reconstruct is recoverable, so the duty is not concluded failed")

	require.NoError(t, runner.ProcessPostConsensus(ctx, logger, msg(4)))
	require.ElementsMatch(t, []uint64{0, 1, 2}, testBeacon.submittedSubnets)
	requireConcluded(t, concluded, dutyOutcomeSucceeded)
}

// A contribution the beacon node rejects doesn't hold the other subnets' contributions back, and fails the duty.
func TestSyncCommitteeAggregatorProcessPostConsensusSubmitsPastARejection(t *testing.T) {
	t.Parallel()

	ctx, logger := t.Context(), zap.NewNop()
	testBeacon := &syncCommitteeContributionSubmitCaptureBeacon{
		BeaconNode:      protocoltesting.NewTestingBeaconNodeWrapped(),
		rejectedSubnets: []uint64{1},
	}
	runner, keySet := newSyncCommitteeAggregatorRunnerForTest(t, testBeacon)
	decideSyncCommitteeContributions(t, runner, keySet)
	concluded := observeDutyConclusion(runner.BaseRunner)
	msg := func(op spectypes.OperatorID) *spectypes.PartialSignatureMessages {
		return spectestingutils.PostConsensusSyncCommitteeContributionMsg(keySet.Shares[op], op, keySet)
	}

	require.NoError(t, runner.ProcessPostConsensus(ctx, logger, msg(1)))
	require.NoError(t, runner.ProcessPostConsensus(ctx, logger, msg(2)))
	err := runner.ProcessPostConsensus(ctx, logger, msg(3))
	require.ErrorContains(t, err, "contribution rejected")
	require.ElementsMatch(t, []uint64{0, 2}, testBeacon.submittedSubnets)
	requireConcluded(t, concluded, dutyOutcomeFailed)
}
