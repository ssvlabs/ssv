package testing

import (
	"bytes"

	"github.com/bloxapp/ssv/protocol/v2/qbft"
	"github.com/bloxapp/ssv/protocol/v2/qbft/controller"
	"github.com/bloxapp/ssv/protocol/v2/qbft/roundtimer"
	"github.com/pkg/errors"
	specqbft "github.com/ssvlabs/ssv-spec/qbft"
	"github.com/ssvlabs/ssv-spec/types"
	"github.com/ssvlabs/ssv-spec/types/testingutils"
	"go.uber.org/zap"
)

var TestingConfig = func(logger *zap.Logger, keySet *testingutils.TestKeySet, role types.RunnerRole) *qbft.Config {
	return &qbft.Config{
		BeaconSigner:   testingutils.NewTestingKeyManager(),
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
		ProposerF: func(state *specqbft.State, round specqbft.Round) types.OperatorID {
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

	// Decode validator public key
	pkBytesSlice := keysSet.ValidatorPK.Serialize()
	pkBytesArray := [48]byte{}
	copy(pkBytesArray[:], pkBytesSlice)

	return &types.Share{
		ValidatorIndex:      testingutils.TestingValidatorIndex,
		ValidatorPubKey:     pkBytesArray,
		SharePubKey:         keysSet.Shares[1].GetPublicKey().Serialize(),
		Committee:           keysSet.Committee(),
		Quorum:              keysSet.Threshold,
		DomainType:          testingutils.TestingSSVDomainType,
		FeeRecipientAddress: testingutils.TestingFeeRecipient,
		Graffiti:            testingutils.TestingGraffiti[:],
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
		config,
		fullNode,
	)
	ctrl.StoredInstances = make(controller.InstanceContainer, 0, controller.InstanceContainerTestCapacity)
	return ctrl
}
