package runner

import (
	"context"
	"testing"

	"github.com/attestantio/go-eth2-client/spec/phase0"
	specqbft "github.com/ssvlabs/ssv-spec/qbft"
	spectypes "github.com/ssvlabs/ssv-spec/types"
	spectestingutils "github.com/ssvlabs/ssv-spec/types/testingutils"
	"go.uber.org/zap"

	"github.com/ssvlabs/ssv/networkconfig"
	"github.com/ssvlabs/ssv/protocol/v2/blockchain/beacon"
	"github.com/ssvlabs/ssv/protocol/v2/qbft/controller"
	"github.com/ssvlabs/ssv/protocol/v2/qbft/roundtimer"
	"github.com/ssvlabs/ssv/protocol/v2/ssv"
	protocoltesting "github.com/ssvlabs/ssv/protocol/v2/testing"
	"github.com/ssvlabs/ssv/ssvsigner/ekm"
)

// runnerTestKit bundles the scaffolding shared by single-validator runner tests
// (proposer, sync-committee aggregator, ...). Each runner-specific helper supplies
// only its role, value checker, and constructor; everything else comes from here.
type runnerTestKit struct {
	cfg            *networkconfig.Network
	keySet         *spectestingutils.TestKeySet
	share          *spectypes.Share
	network        *spectestingutils.TestingNetwork
	signer         ekm.BeaconSigner
	qbftController *controller.Controller
	baseOptions    BaseRunnerOptions
}

// newRunnerTestKit builds the common single-validator runner scaffolding for role,
// wiring bn as the runner's beacon node. If cfg is nil a fresh cloneTestNetworkConfig()
// is used, so parallel tests can mutate beacon timing without racing on the shared global.
func newRunnerTestKit(t *testing.T, role spectypes.RunnerRole, bn beacon.BeaconNode, cfg *networkconfig.Network) *runnerTestKit {
	t.Helper()

	if cfg == nil {
		cfg = cloneTestNetworkConfig()
	}

	logger := zap.NewNop()
	keySet := spectestingutils.Testing4SharesSet()
	share := spectestingutils.TestingShare(keySet, spectestingutils.TestingValidatorIndex)
	identifier := spectypes.NewMsgID(spectypes.JatoTestnet, spectestingutils.TestingValidatorPubKey[:], role)
	network := spectestingutils.NewTestingNetwork(1, keySet.OperatorKeys[1])
	km := ekm.NewTestingKeyManagerAdapter(spectestingutils.NewTestingKeyManager())
	operator := spectestingutils.TestingCommitteeMember(keySet)
	operatorSigner := spectestingutils.NewOperatorSigner(keySet, 1)

	qbftConfig := protocoltesting.TestingConfig(logger, keySet)
	qbftConfig.ProposerF = func(state *specqbft.State, round specqbft.Round) spectypes.OperatorID {
		return 1
	}
	qbftConfig.Network = network
	qbftConfig.BeaconSigner = km

	ctrl := protocoltesting.NewTestingQBFTController(
		keySet,
		identifier[:],
		operator,
		qbftConfig,
		false,
	)

	shareMap := map[phase0.ValidatorIndex]*spectypes.Share{
		share.ValidatorIndex: share,
	}

	return &runnerTestKit{
		cfg:            cfg,
		keySet:         keySet,
		share:          share,
		network:        network,
		signer:         km,
		qbftController: ctrl,
		baseOptions: BaseRunnerOptions{
			NetworkConfig:  cfg,
			Share:          shareMap,
			Beacon:         bn,
			Network:        network,
			Signer:         km,
			OperatorSigner: operatorSigner,
		},
	}
}

// testingRoundTimerF is the no-wait QBFT round timer factory shared by runner tests.
func testingRoundTimerF(_ context.Context, _ *zap.Logger, _ phase0.Slot) ssv.QBFTRoundTimer {
	return roundtimer.NewTestingTimer()
}
