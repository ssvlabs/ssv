package runner

import (
	"context"
	"slices"
	"testing"

	"github.com/attestantio/go-eth2-client/spec"
	"github.com/attestantio/go-eth2-client/spec/phase0"
	ssz "github.com/ferranbt/fastssz"
	spectypes "github.com/ssvlabs/ssv-spec/types"
	spectestingutils "github.com/ssvlabs/ssv-spec/types/testingutils"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	"github.com/ssvlabs/ssv/protocol/v2/blockchain/beacon"
	"github.com/ssvlabs/ssv/protocol/v2/ssv"
	protocoltesting "github.com/ssvlabs/ssv/protocol/v2/testing"
	ssvtypes "github.com/ssvlabs/ssv/protocol/v2/types"
)

// legacyTestingSyncCommitteeContributionDuty reconstructs the pre-AggregatorCommittee, per-validator
// sync committee contribution duty fixture that v1.2.2's testingutils.TestingSyncCommitteeContributionDuty
// used to provide. v1.2.3 replaced it with a batched types.AggregatorCommitteeDuty fixture, which our
// (not yet merged, see convergence unit 5) SyncCommitteeAggregatorRunner cannot consume.
var legacyTestingSyncCommitteeContributionDuty = spectypes.ValidatorDuty{
	Type:                          spectypes.BNRoleSyncCommitteeContribution,
	PubKey:                        spectestingutils.TestingValidatorPubKey,
	Slot:                          spectestingutils.TestingDutySlot,
	ValidatorIndex:                spectestingutils.TestingValidatorIndex,
	CommitteeIndex:                3,
	CommitteesAtSlot:              36,
	CommitteeLength:               128,
	ValidatorCommitteeIndex:       11,
	ValidatorSyncCommitteeIndices: spectestingutils.TestingContributionProofIndexes,
}

// syncCommitteeContributionTestBeacon embeds the shared testing beacon node and
// captures the (subnetIDs, selectionProofs) actually handed to
// GetSyncCommitteeContribution, so a test can assert the runner passes them in
// canonical (ascending-subnet) order.
type syncCommitteeContributionTestBeacon struct {
	beacon.BeaconNode

	getContributionCalls    int
	capturedSubnets         []uint64
	capturedSelectionProofs []phase0.BLSSignature
}

func newSyncCommitteeContributionTestBeacon() *syncCommitteeContributionTestBeacon {
	return &syncCommitteeContributionTestBeacon{
		BeaconNode: protocoltesting.NewTestingBeaconNodeWrapped(),
	}
}

func (b *syncCommitteeContributionTestBeacon) GetSyncCommitteeContribution(
	_ context.Context,
	_ phase0.Slot,
	selectionProofs []phase0.BLSSignature,
	subnetIDs []uint64,
) (ssz.Marshaler, spec.DataVersion, error) {
	b.getContributionCalls++
	b.capturedSubnets = append([]uint64(nil), subnetIDs...)
	b.capturedSelectionProofs = append([]phase0.BLSSignature(nil), selectionProofs...)
	// Return a valid Contributions value so the runner can proceed into consensus;
	// the captured args above are what the test asserts on.
	return &spectestingutils.TestingContributionsData, spec.DataVersionAltair, nil
}

// TestSyncCommitteeAggregatorProcessPreConsensusSortsSubnetsForBeaconCall is the
// integration-level guard for the determinism fix: it drives a full pre-consensus
// quorum through ProcessPreConsensus and asserts that the subnets actually passed to
// GetSyncCommitteeContribution are in ascending (spec-canonical) order.
//
// It complements the TestSortBySubnet unit test by pinning the fix at the wiring boundary,
// where the non-determinism actually lives: the upstream `roots` come from
// slices.Collect(maps.Keys(...)) (random per process) and must be canonicalized before the
// beacon call. A helper-only test can't catch a dropped or misplaced sort call; this one can.
// ssv-spec's runner tests can't either — their TestingBeaconNode.GetSyncCommitteeContribution
// discards subnetIDs and returns a pre-sorted fixture.
func TestSyncCommitteeAggregatorProcessPreConsensusSortsSubnetsForBeaconCall(t *testing.T) {
	testBeacon := newSyncCommitteeContributionTestBeacon()
	runner, keySet := newSyncCommitteeAggregatorRunnerForTest(t, testBeacon)

	// Copy the shared fixture so we never alias/mutate package state across tests.
	//
	// NOTE(convergence unit 0): v1.2.3 ssv-spec removed the per-validator
	// testingutils.TestingSyncCommitteeContributionDuty fixture in favor of a batched
	// AggregatorCommitteeDuty one; legacyTestingSyncCommitteeContributionDuty reconstructs
	// the old fixture shape so this (not yet merged, see unit 5) runner test keeps compiling.
	dutyVal := legacyTestingSyncCommitteeContributionDuty
	require.NoError(t, runner.StartNewDuty(context.Background(), zap.NewNop(), &dutyVal, keySet.Threshold))

	ctx := context.Background()
	logger := zap.NewNop()
	for operatorID := spectypes.OperatorID(1); operatorID <= keySet.Threshold; operatorID++ {
		msg := spectestingutils.PreConsensusContributionProofMsg(
			keySet.Shares[operatorID], keySet.Shares[operatorID], operatorID, operatorID,
		)
		require.NoError(t, runner.ProcessPreConsensus(ctx, logger, msg))
	}

	require.Equal(t, 1, testBeacon.getContributionCalls,
		"GetSyncCommitteeContribution should be called exactly once, when pre-consensus quorum is reached")

	// Sortedness is the invariant the determinism fix guarantees: without the sort the runner
	// would hand the beacon node a random permutation of the upstream map-iteration order.
	require.Truef(t, slices.IsSorted(testBeacon.capturedSubnets),
		"subnets passed to GetSyncCommitteeContribution must be ascending (spec-canonical), got %v",
		testBeacon.capturedSubnets)
	// Exact-set sanity check: TestingContributionProofIndexes {0,1,2} map to subnets {0,1,2}
	// (ssv-spec/types/testingutils/beacon_node_sync_committee.go). A future fixture change trips
	// this line specifically — update it here, not the sortedness assertion above.
	require.Equal(t, []uint64{0, 1, 2}, testBeacon.capturedSubnets)
	// One proof per subnet reaches the beacon node; the (subnet, proof) pairing is exercised
	// exhaustively by TestSortBySubnet. Here we just confirm none were dropped.
	require.Len(t, testBeacon.capturedSelectionProofs, len(testBeacon.capturedSubnets))

	// Sanity: the runner proceeded into consensus after the beacon call.
	require.NotNil(t, runner.State.RunningInstance)
}

func newSyncCommitteeAggregatorRunnerForTest(
	t *testing.T,
	testBeacon *syncCommitteeContributionTestBeacon,
) (*SyncCommitteeAggregatorRunner, *spectestingutils.TestKeySet) {
	t.Helper()

	kit := newRunnerTestKit(t, ssvtypes.RoleSyncCommitteeContribution, testBeacon, nil)
	valCheck := ssv.NewSyncCommitteeContributionChecker(
		kit.cfg.Beacon,
		spectypes.ValidatorPK(spectestingutils.TestingValidatorPubKey),
		spectestingutils.TestingValidatorIndex,
	)

	runnerIface, err := NewSyncCommitteeAggregatorRunner(SyncCommitteeAggregatorRunnerOptions{
		BaseRunnerOptions:  kit.baseOptions,
		QBFTController:     kit.qbftController,
		ValCheck:           valCheck,
		HighestDecidedSlot: 0,
	})
	require.NoError(t, err)

	runner := runnerIface.(*SyncCommitteeAggregatorRunner)
	runner.SetQBFTRoundTimerF(testingRoundTimerF)
	return runner, kit.keySet
}
