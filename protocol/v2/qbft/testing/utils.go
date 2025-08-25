package testing

import (
	"bytes"

	"github.com/pkg/errors"
	specqbft "github.com/ssvlabs/ssv-spec/qbft"
	"github.com/ssvlabs/ssv-spec/types"
	"github.com/ssvlabs/ssv-spec/types/testingutils"
	"go.uber.org/zap"

	"github.com/ssvlabs/ssv/protocol/v2/qbft"
	"github.com/ssvlabs/ssv/protocol/v2/qbft/controller"
	"github.com/ssvlabs/ssv/protocol/v2/qbft/roundtimer"
	"github.com/ssvlabs/ssv/ssvsigner/ekm"
)

var TestingConfig = func(logger *zap.Logger, keySet *testingutils.TestKeySet) *qbft.Config {
	return &qbft.Config{
		BeaconSigner: ekm.NewTestingKeyManagerAdapter(testingutils.NewTestingKeyManager()),
		Domain:       testingutils.TestingSSVDomainType,
		ValueCheckF: func(data []byte) error {
			if bytes.Equal(data, TestingInvalidValueCheck) {
				return errors.New("invalid value")
			}

			// as a base validation we do not accept nil values
			if len(data) == 0 {
				return errors.New("invalid value")
			}
			return nil
		},
		ProposerF: func(state *specqbft.State, round specqbft.Round) types.OperatorID {
			return 1
		},
		Network:     testingutils.NewTestingNetwork(1, keySet.OperatorKeys[1]),
		Timer:       roundtimer.NewTestingTimer(),
		CutOffRound: testingutils.TestingCutOffRound,
	}
}

var TestingInvalidValueCheck = []byte{1, 1, 1, 1}

var BaseInstance = func() *specqbft.Instance {
	return baseInstance(testingutils.TestingCommitteeMember(testingutils.Testing4SharesSet()), testingutils.Testing4SharesSet(), []byte{1, 2, 3, 4})
}

var SevenOperatorsInstance = func() *specqbft.Instance {
	return baseInstance(testingutils.TestingCommitteeMember(testingutils.Testing7SharesSet()), testingutils.Testing7SharesSet(), []byte{1, 2, 3, 4})
}

var TenOperatorsInstance = func() *specqbft.Instance {
	return baseInstance(testingutils.TestingCommitteeMember(testingutils.Testing10SharesSet()), testingutils.Testing10SharesSet(), []byte{1, 2, 3, 4})
}

var ThirteenOperatorsInstance = func() *specqbft.Instance {
	return baseInstance(testingutils.TestingCommitteeMember(testingutils.Testing13SharesSet()), testingutils.Testing13SharesSet(), []byte{1, 2, 3, 4})
}

var baseInstance = func(share *types.CommitteeMember, keySet *testingutils.TestKeySet, identifier []byte) *specqbft.Instance {
	ret := specqbft.NewInstance(testingutils.TestingConfig(keySet), share, identifier, specqbft.FirstHeight, testingutils.TestingOperatorSigner(keySet))
	ret.StartValue = []byte{1, 2, 3, 4}
	return ret
}

func NewTestingQBFTController(
	keySet *testingutils.TestKeySet,
	identifier []byte,
	share *types.CommitteeMember,
	config qbft.IConfig,
	fullNode bool,
) *controller.Controller {
	ctrl := controller.NewController(
		identifier,
		share,
		config,
		testingutils.TestingOperatorSigner(keySet),
		fullNode,
	)
	ctrl.StoredInstances = make(controller.InstanceContainer, 0, controller.InstanceContainerTestCapacity)
	return ctrl
}
