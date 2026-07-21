package runner

import (
	"context"
	"slices"
	"testing"

	"github.com/attestantio/go-eth2-client/spec"
	"github.com/attestantio/go-eth2-client/spec/phase0"
	ssz "github.com/ferranbt/fastssz"
	specqbft "github.com/ssvlabs/ssv-spec/qbft"
	spectypes "github.com/ssvlabs/ssv-spec/types"
	spectestingutils "github.com/ssvlabs/ssv-spec/types/testingutils"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	"github.com/ssvlabs/ssv/networkconfig"
	"github.com/ssvlabs/ssv/protocol/v2/blockchain/beacon"
	"github.com/ssvlabs/ssv/protocol/v2/qbft/roundtimer"
	"github.com/ssvlabs/ssv/protocol/v2/ssv"
	protocoltesting "github.com/ssvlabs/ssv/protocol/v2/testing"
	ssvtypes "github.com/ssvlabs/ssv/protocol/v2/types"
	"github.com/ssvlabs/ssv/ssvsigner/ekm"
)

// syncCommitteeContributionPreConsensusCaptureBeacon embeds the shared testing beacon node and
// captures the (selectionProofs, subnetIDs) actually handed to GetSyncCommitteeContribution, so a
// test can assert the runner passes them in canonical (ascending-subnet) order.
type syncCommitteeContributionPreConsensusCaptureBeacon struct {
	beacon.BeaconNode

	getContributionCalls    int
	capturedSubnets         []uint64
	capturedSelectionProofs []phase0.BLSSignature
}

func newSyncCommitteeContributionPreConsensusCaptureBeacon() *syncCommitteeContributionPreConsensusCaptureBeacon {
	return &syncCommitteeContributionPreConsensusCaptureBeacon{
		BeaconNode: protocoltesting.NewTestingBeaconNodeWrapped(),
	}
}

func (b *syncCommitteeContributionPreConsensusCaptureBeacon) GetSyncCommitteeContribution(
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

// syncCommitteeContributionValidatorSyncCommitteeIndices spans all 4 mainnet sync-committee
// subnets (subnetSize = 512/4 = 128, see ssv-spec TestingBeaconNode.SyncCommitteeSubnetID), so the
// pre-consensus quorum below produces 4 distinct (subnet, selection-proof) pairs. Using all 4
// subnets (rather than stage's original 3-subnet fixture) shrinks the odds that the
// non-deterministic map iteration in basePreConsensusMsgProcessing happens to already come out
// sorted (1/4! per draw instead of 1/3!), which matters because the test below also re-drives the
// quorum across several fresh runners to make an accidental false-negative in the mutation check
// (see TestSyncCommitteeAggregatorProcessPreConsensusSortsSubnetsForBeaconCall) vanishingly unlikely.
var syncCommitteeContributionValidatorSyncCommitteeIndices = []spectypes.ValidatorSyncCommitteeIndex{0, 128, 256, 384}

// TestSyncCommitteeAggregatorProcessPreConsensusSortsSubnetsForBeaconCall is the runner-flow-level
// regression guard for the determinism fix in sync_committee_contribution.go: it drives a full
// pre-consensus quorum through ProcessPreConsensus and asserts that the subnets actually passed to
// GetSyncCommitteeContribution are in ascending (spec-canonical) order, even though the upstream
// `roots` come from slices.Collect(maps.Keys(...)) (randomized per range) in
// basePreConsensusMsgProcessing.
//
// It complements the TestSortBySubnet helper-level unit test by pinning the fix at the wiring
// boundary where the non-determinism actually lives: a helper-only test can't catch a dropped or
// misplaced sortBySubnet call at the ProcessPreConsensus call site; this one can. Re-added per
// ssvlabs/ssv#2952 item 8 after the Boole convergence (#2941) deleted stage's runner test kit and
// the flow-level test that depended on it.
//
// Because Go's map-iteration randomization is per-range (not fixed for the process), a single
// draw could in principle land on already-sorted order and let a missing sortBySubnet call slip
// through undetected; the sub-test below re-drives the whole flow on fresh runners several times to
// make that false negative astronomically unlikely, and the mutation check documented in this
// package's test-authoring report empirically confirms the guard trips when the sort is removed.
func TestSyncCommitteeAggregatorProcessPreConsensusSortsSubnetsForBeaconCall(t *testing.T) {
	t.Parallel()

	const draws = 25
	for i := range draws {
		testBeacon := newSyncCommitteeContributionPreConsensusCaptureBeacon()
		runner, keySet := newSyncCommitteeAggregatorRunnerForTest(t, testBeacon)

		duty := &spectypes.ValidatorDuty{
			Type:                          spectypes.BNRoleSyncCommitteeContribution,
			PubKey:                        spectestingutils.TestingValidatorPubKey,
			Slot:                          spectestingutils.TestingDutySlot,
			ValidatorIndex:                spectestingutils.TestingValidatorIndex,
			ValidatorSyncCommitteeIndices: syncCommitteeContributionValidatorSyncCommitteeIndices,
		}
		require.NoError(t, runner.StartNewDuty(context.Background(), zap.NewNop(), duty, keySet.Threshold))

		ctx := context.Background()
		logger := zap.NewNop()
		for operatorID := spectypes.OperatorID(1); operatorID <= keySet.Threshold; operatorID++ {
			msg := spectestingutils.PreConsensusContributionProofWithValidatorSyncCommitteeIndices(
				keySet.Shares[operatorID], keySet.Shares[operatorID], operatorID, operatorID,
				syncCommitteeContributionValidatorSyncCommitteeIndices,
			)
			require.NoError(t, runner.ProcessPreConsensus(ctx, logger, msg))
		}

		require.Equal(t, 1, testBeacon.getContributionCalls,
			"draw %d: GetSyncCommitteeContribution should be called exactly once, when pre-consensus quorum is reached", i)

		// Sortedness is the invariant the determinism fix guarantees: without the sort the runner
		// would hand the beacon node a random permutation of the upstream map-iteration order.
		require.Truef(t, slices.IsSorted(testBeacon.capturedSubnets),
			"draw %d: subnets passed to GetSyncCommitteeContribution must be ascending (spec-canonical), got %v",
			i, testBeacon.capturedSubnets)
		require.Equal(t, []uint64{0, 1, 2, 3}, testBeacon.capturedSubnets, "draw %d: exact-set sanity check", i)
		require.Len(t, testBeacon.capturedSelectionProofs, len(testBeacon.capturedSubnets), "draw %d", i)

		// Sanity: the runner proceeded into consensus after the beacon call.
		require.NotNil(t, runner.State.RunningInstance, "draw %d", i)
	}
}

func newSyncCommitteeAggregatorRunnerForTest(
	t *testing.T,
	testBeacon *syncCommitteeContributionPreConsensusCaptureBeacon,
) (*SyncCommitteeAggregatorRunner, *spectestingutils.TestKeySet) {
	t.Helper()

	logger := zap.NewNop()
	keySet := spectestingutils.Testing4SharesSet()
	share := spectestingutils.TestingShare(keySet, spectestingutils.TestingValidatorIndex)
	identifier := spectypes.NewMsgID(spectypes.JatoTestnet, spectestingutils.TestingValidatorPubKey[:], ssvtypes.RoleSyncCommitteeContribution)
	network := protocoltesting.NewTestingNetwork(1, keySet.OperatorKeys[1])
	km := ekm.NewTestingKeyManagerAdapter(spectestingutils.NewTestingKeyManager())
	operator := spectestingutils.TestingCommitteeMember(keySet)
	operatorSigner := spectestingutils.NewOperatorSigner(keySet, 1)
	valCheck := ssv.NewSyncCommitteeContributionChecker(
		networkconfig.TestNetwork.Beacon,
		spectypes.ValidatorPK(spectestingutils.TestingValidatorPubKey),
		spectestingutils.TestingValidatorIndex,
	)

	qbftConfig := protocoltesting.TestingConfig(logger, keySet)
	qbftConfig.ProposerF = func(state *specqbft.State, round specqbft.Round) spectypes.OperatorID {
		return 1
	}
	qbftConfig.Network = network
	controller := protocoltesting.NewTestingQBFTController(keySet, identifier[:], operator, qbftConfig, false)

	shareMap := map[phase0.ValidatorIndex]*spectypes.Share{
		share.ValidatorIndex: share,
	}

	runnerIface, err := NewSyncCommitteeAggregatorRunner(SyncCommitteeAggregatorRunnerOptions{
		BaseRunnerOptions: BaseRunnerOptions{
			NetworkConfig:  networkconfig.TestNetwork,
			Share:          shareMap,
			Beacon:         testBeacon,
			Network:        network,
			Signer:         km,
			OperatorSigner: operatorSigner,
		},
		QBFTController:     controller,
		ValCheck:           valCheck,
		HighestDecidedSlot: 0,
	})
	require.NoError(t, err)

	runner := runnerIface.(*SyncCommitteeAggregatorRunner)
	runner.SetQBFTRoundTimerF(func(_ context.Context, _ *zap.Logger, _ phase0.Slot) ssv.QBFTRoundTimer {
		return roundtimer.NewTestingTimer()
	})
	return runner, keySet
}
