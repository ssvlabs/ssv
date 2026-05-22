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

	"github.com/ssvlabs/ssv/protocol/v2/blockchain/beacon"
	"github.com/ssvlabs/ssv/protocol/v2/qbft/roundtimer"
	"github.com/ssvlabs/ssv/protocol/v2/ssv"
	protocoltesting "github.com/ssvlabs/ssv/protocol/v2/testing"
	"github.com/ssvlabs/ssv/ssvsigner/ekm"
)

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
// Why this exists on top of the TestSortBySubnet unit test: the regression that
// motivated the fix (#2675) was in the *wiring*, not in a sort helper — the upstream
// `roots` come from slices.Collect(maps.Keys(...)) (random per process), and the bug
// was that the order was never canonicalized before the beacon call. A helper-only
// test cannot catch a dropped or misplaced sort call; this one can, because it pins
// the contract at the GetSyncCommitteeContribution boundary. ssv-spec's runner tests
// cannot catch it either: their TestingBeaconNode.GetSyncCommitteeContribution
// discards subnetIDs and returns a pre-sorted fixture.
func TestSyncCommitteeAggregatorProcessPreConsensusSortsSubnetsForBeaconCall(t *testing.T) {
	testBeacon := newSyncCommitteeContributionTestBeacon()
	runner, keySet := newSyncCommitteeAggregatorRunnerForTest(t, testBeacon)

	// Copy the shared fixture so we never alias/mutate package state across tests.
	dutyVal := spectestingutils.TestingSyncCommitteeContributionDuty
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

	// TestingContributionProofIndexes = {0,1,2} map to subnets {0,1,2}; with the fix
	// the runner hands them to the beacon node in ascending order regardless of the
	// random map-iteration order of the upstream roots slice. Without the sort this
	// assertion would observe a random permutation and fail.
	require.Equal(t, []uint64{0, 1, 2}, testBeacon.capturedSubnets,
		"subnets passed to GetSyncCommitteeContribution must be in canonical ascending order")
	require.True(t, slices.IsSorted(testBeacon.capturedSubnets))
	// One proof per subnet reaches the beacon node. The (subnet, proof) pairing itself
	// is exercised exhaustively by TestSortBySubnet; here we just confirm none are dropped.
	require.Len(t, testBeacon.capturedSelectionProofs, len(testBeacon.capturedSubnets))

	// Sanity: the runner proceeded into consensus after the beacon call.
	require.NotNil(t, runner.State.RunningInstance)
}

func newSyncCommitteeAggregatorRunnerForTest(
	t *testing.T,
	testBeacon *syncCommitteeContributionTestBeacon,
) (*SyncCommitteeAggregatorRunner, *spectestingutils.TestKeySet) {
	t.Helper()

	cfg := cloneTestNetworkConfig()
	logger := zap.NewNop()
	keySet := spectestingutils.Testing4SharesSet()
	share := spectestingutils.TestingShare(keySet, spectestingutils.TestingValidatorIndex)
	identifier := spectypes.NewMsgID(spectypes.JatoTestnet, spectestingutils.TestingValidatorPubKey[:], spectypes.RoleSyncCommitteeContribution)
	network := spectestingutils.NewTestingNetwork(1, keySet.OperatorKeys[1])
	km := ekm.NewTestingKeyManagerAdapter(spectestingutils.NewTestingKeyManager())
	operator := spectestingutils.TestingCommitteeMember(keySet)
	operatorSigner := spectestingutils.NewOperatorSigner(keySet, 1)
	valCheck := ssv.NewSyncCommitteeContributionChecker(
		cfg.Beacon,
		spectypes.ValidatorPK(spectestingutils.TestingValidatorPubKey),
		spectestingutils.TestingValidatorIndex,
	)

	qbftConfig := protocoltesting.TestingConfig(logger, keySet)
	qbftConfig.ProposerF = func(state *specqbft.State, round specqbft.Round) spectypes.OperatorID {
		return 1
	}
	qbftConfig.Network = network
	qbftConfig.BeaconSigner = km

	controller := protocoltesting.NewTestingQBFTController(
		keySet,
		identifier[:],
		operator,
		qbftConfig,
		false,
	)

	shareMap := map[phase0.ValidatorIndex]*spectypes.Share{
		share.ValidatorIndex: share,
	}

	runnerIface, err := NewSyncCommitteeAggregatorRunner(SyncCommitteeAggregatorRunnerOptions{
		BaseRunnerOptions: BaseRunnerOptions{
			NetworkConfig:  cfg,
			Share:          shareMap,
			Beacon:         testBeacon,
			Network:        network,
			Signer:         km,
			OperatorSigner: operatorSigner,
		},
		QBFTController:     controller,
		ValCheck:          valCheck,
		HighestDecidedSlot: 0,
	})
	require.NoError(t, err)

	runner := runnerIface.(*SyncCommitteeAggregatorRunner)
	runner.SetQBFTRoundTimerF(func(_ context.Context, _ *zap.Logger, _ phase0.Slot) ssv.QBFTRoundTimer {
		return roundtimer.NewTestingTimer()
	})
	return runner, keySet
}
