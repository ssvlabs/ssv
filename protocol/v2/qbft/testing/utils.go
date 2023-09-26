package testing

import (
	"bytes"

	specqbft "github.com/bloxapp/ssv-spec/qbft"
	"github.com/bloxapp/ssv-spec/types"
	"github.com/bloxapp/ssv-spec/types/testingutils"

	"github.com/pkg/errors"
	"go.uber.org/zap"

	"github.com/bloxapp/ssv/protocol/v2/qbft"
	"github.com/bloxapp/ssv/protocol/v2/qbft/controller"
	"github.com/bloxapp/ssv/protocol/v2/qbft/roundtimer"
)

var TestingConfig = func(logger *zap.Logger, keySet *testingutils.TestKeySet, role types.BeaconRole) *qbft.Config {
	return &qbft.Config{
		Signer:    testingutils.NewTestingKeyManager(),
		SigningPK: keySet.Shares[1].GetPublicKey().Serialize(),
		Domain:    testingutils.TestingSSVDomainType,
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
		Storage:               TestingStores(logger).Get(role),
		Network:               testingutils.NewTestingNetwork(),
		Timer:                 roundtimer.NewTestingTimer(),
		SignatureVerification: true,
	}
}

var TestingInvalidValueCheck = []byte{1, 1, 1, 1}

var TestingShare = func(keysSet *testingutils.TestKeySet) *types.Share {
	return &types.Share{
		OperatorID:      1,
		ValidatorPubKey: keysSet.ValidatorPK.Serialize(),
		SharePubKey:     keysSet.Shares[1].GetPublicKey().Serialize(),
		DomainType:      testingutils.TestingSSVDomainType,
		Quorum:          keysSet.Threshold,
		PartialQuorum:   keysSet.PartialThreshold,
		Committee:       keysSet.Committee(),
	}
}

var BaseInstance = func() *specqbft.Instance {
	return baseInstance(TestingShare(testingutils.Testing4SharesSet()), testingutils.Testing4SharesSet(), []byte{1, 2, 3, 4})
}

var SevenOperatorsInstance = func() *specqbft.Instance {
	return baseInstance(TestingShare(testingutils.Testing7SharesSet()), testingutils.Testing7SharesSet(), []byte{1, 2, 3, 4})
}

var TenOperatorsInstance = func() *specqbft.Instance {
	return baseInstance(TestingShare(testingutils.Testing10SharesSet()), testingutils.Testing10SharesSet(), []byte{1, 2, 3, 4})
}

var ThirteenOperatorsInstance = func() *specqbft.Instance {
	return baseInstance(TestingShare(testingutils.Testing13SharesSet()), testingutils.Testing13SharesSet(), []byte{1, 2, 3, 4})
}

var baseInstance = func(share *types.Share, keySet *testingutils.TestKeySet, identifier []byte) *specqbft.Instance {
	ret := specqbft.NewInstance(testingutils.TestingConfig(keySet), share, identifier, specqbft.FirstHeight)
	ret.StartValue = []byte{1, 2, 3, 4}
	return ret
}

func NewTestingQBFTController(
	identifier []byte,
	share *types.Share,
	config qbft.IConfig,
	fullNode bool,
) *controller.Controller {
	ctrl := controller.NewController(
		identifier,
		share,
		testingutils.TestingSSVDomainType,
		config,
		fullNode,
	)
	ctrl.StoredInstances = make(controller.InstanceContainer, 0, controller.InstanceContainerTestCapacity)
	return ctrl
}
