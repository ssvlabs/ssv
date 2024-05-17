package testing

import (
	"bytes"

	specqbft "github.com/ssvlabs/ssv-spec/qbft"
	"github.com/ssvlabs/ssv-spec/types"
	"github.com/ssvlabs/ssv-spec/types/testingutils"

	"github.com/pkg/errors"
	"go.uber.org/zap"

	"github.com/bloxapp/ssv/protocol/v2/qbft"
	"github.com/bloxapp/ssv/protocol/v2/qbft/controller"
	"github.com/bloxapp/ssv/protocol/v2/qbft/roundtimer"
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

var TestingOperator = func(keysSet *testingutils.TestKeySet) *types.Operator {
	committeeMembers := []*types.CommitteeMember{}

	for _, key := range keysSet.Committee() {

		// Encode member's public key
		pkBytes, err := types.MarshalPublicKey(keysSet.OperatorKeys[key.Signer])
		if err != nil {
			panic(err)
		}

		committeeMembers = append(committeeMembers, &types.CommitteeMember{
			OperatorID:        key.Signer,
			SSVOperatorPubKey: pkBytes,
		})
	}

	opIds := []types.OperatorID{}
	for _, key := range keysSet.Committee() {
		opIds = append(opIds, key.Signer)
	}

	operatorPubKeyBytes, err := types.MarshalPublicKey(keysSet.OperatorKeys[1])
	if err != nil {
		panic(err)
	}

	return &types.Operator{
		OperatorID:        1,
		ClusterID:         types.GetCommitteeID(opIds),
		SSVOperatorPubKey: operatorPubKeyBytes,
		Quorum:            keysSet.Threshold,
		PartialQuorum:     keysSet.PartialThreshold,
		Committee:         committeeMembers,
	}
}

var BaseInstance = func() *specqbft.Instance {
	return baseInstance(TestingOperator(testingutils.Testing4SharesSet()), testingutils.Testing4SharesSet(), []byte{1, 2, 3, 4})
}

var SevenOperatorsInstance = func() *specqbft.Instance {
	return baseInstance(TestingOperator(testingutils.Testing7SharesSet()), testingutils.Testing7SharesSet(), []byte{1, 2, 3, 4})
}

var TenOperatorsInstance = func() *specqbft.Instance {
	return baseInstance(TestingOperator(testingutils.Testing10SharesSet()), testingutils.Testing10SharesSet(), []byte{1, 2, 3, 4})
}

var ThirteenOperatorsInstance = func() *specqbft.Instance {
	return baseInstance(TestingOperator(testingutils.Testing13SharesSet()), testingutils.Testing13SharesSet(), []byte{1, 2, 3, 4})
}

var baseInstance = func(share *types.Operator, keySet *testingutils.TestKeySet, identifier []byte) *specqbft.Instance {
	ret := specqbft.NewInstance(testingutils.TestingConfig(keySet), share, identifier, specqbft.FirstHeight)
	ret.StartValue = []byte{1, 2, 3, 4}
	return ret
}

func NewTestingQBFTController(
	identifier []byte,
	share *types.Operator,
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
