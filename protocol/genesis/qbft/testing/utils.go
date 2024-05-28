package testing

import (
	"bytes"

	"github.com/pkg/errors"
	"go.uber.org/zap"

	"github.com/ssvlabs/ssv/protocol/genesis/qbft"
	"github.com/ssvlabs/ssv/protocol/genesis/qbft/controller"
	"github.com/ssvlabs/ssv/protocol/genesis/qbft/roundtimer"
	types2 "github.com/ssvlabs/ssv/protocol/genesis/types"

	genesisspecqbft "github.com/ssvlabs/ssv-spec-pre-cc/qbft"
	"github.com/ssvlabs/ssv-spec-pre-cc/types"
	"github.com/ssvlabs/ssv-spec-pre-cc/types/testingutils"
)

var TestingConfig = func(logger *zap.Logger, keySet *testingutils.TestKeySet, role types.BeaconRole) *qbft.Config {
	return &qbft.Config{
		ShareSigner:    testingutils.NewTestingKeyManager(),
		OperatorSigner: testingutils.NewTestingOperatorSigner(keySet, 1),
		SigningPK:      keySet.Shares[1].GetPublicKey().Serialize(),
		Domain:         testingutils.TestingSSVDomainType,
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
		ProposerF: func(state *types2.State, round genesisspecqbft.Round) types.OperatorID {
			return 1
		},
		Storage:               TestingStores(logger).Get(role),
		Network:               testingutils.NewTestingNetwork(1, keySet.OperatorKeys[1]),
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

var BaseInstance = func() *genesisspecqbft.Instance {
	return baseInstance(TestingShare(testingutils.Testing4SharesSet()), testingutils.Testing4SharesSet(), []byte{1, 2, 3, 4})
}

var SevenOperatorsInstance = func() *genesisspecqbft.Instance {
	return baseInstance(TestingShare(testingutils.Testing7SharesSet()), testingutils.Testing7SharesSet(), []byte{1, 2, 3, 4})
}

var TenOperatorsInstance = func() *genesisspecqbft.Instance {
	return baseInstance(TestingShare(testingutils.Testing10SharesSet()), testingutils.Testing10SharesSet(), []byte{1, 2, 3, 4})
}

var ThirteenOperatorsInstance = func() *genesisspecqbft.Instance {
	return baseInstance(TestingShare(testingutils.Testing13SharesSet()), testingutils.Testing13SharesSet(), []byte{1, 2, 3, 4})
}

var baseInstance = func(share *types.Share, keySet *testingutils.TestKeySet, identifier []byte) *genesisspecqbft.Instance {
	ret := genesisspecqbft.NewInstance(testingutils.TestingConfig(keySet), share, identifier, genesisspecqbft.FirstHeight)
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
		config,
		fullNode,
	)
	ctrl.StoredInstances = make(controller.InstanceContainer, 0, controller.InstanceContainerTestCapacity)
	return ctrl
}
