package testing

import (
	"bytes"

	"github.com/pkg/errors"
	genesisspecqbft "github.com/ssvlabs/ssv-spec-pre-cc/qbft"
	genesisspectypes "github.com/ssvlabs/ssv-spec-pre-cc/types"
	genesistestingutils "github.com/ssvlabs/ssv-spec-pre-cc/types/testingutils"
	spectypes "github.com/ssvlabs/ssv-spec/types"
	"github.com/ssvlabs/ssv-spec/types/testingutils"
	"go.uber.org/zap"

	"github.com/ssvlabs/ssv/protocol/genesis/qbft"
	"github.com/ssvlabs/ssv/protocol/genesis/qbft/controller"
	"github.com/ssvlabs/ssv/protocol/genesis/qbft/roundtimer"
	genesistypes "github.com/ssvlabs/ssv/protocol/genesis/types"
)

var TestingConfig = func(logger *zap.Logger, keySet *testingutils.TestKeySet, role genesisspectypes.BeaconRole) *qbft.Config {
	return &qbft.Config{
		ShareSigner:    genesistestingutils.NewTestingKeyManager(),
		OperatorSigner: qbft.NewTestingOperatorSigner(keySet, 1),
		SigningPK:      keySet.Shares[1].GetPublicKey().Serialize(),
		Domain:         genesistestingutils.TestingSSVDomainType,
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
		ProposerF: func(state *genesistypes.State, round genesisspecqbft.Round) genesisspectypes.OperatorID {
			return 1
		},
		Storage:               TestingStores(logger).Get(role),
		Network:               genesistestingutils.NewTestingNetwork(1, keySet.OperatorKeys[1]),
		Timer:                 roundtimer.NewTestingTimer(),
		SignatureVerification: true,
	}
}

var TestingInvalidValueCheck = []byte{1, 1, 1, 1}

var TestingShare = func(keysSet *genesistestingutils.TestKeySet) *genesisspectypes.Share {
	return &genesisspectypes.Share{
		OperatorID:      1,
		ValidatorPubKey: keysSet.ValidatorPK.Serialize(),
		SharePubKey:     keysSet.Shares[1].GetPublicKey().Serialize(),
		DomainType:      genesistestingutils.TestingSSVDomainType,
		Quorum:          keysSet.Threshold,
		PartialQuorum:   keysSet.PartialThreshold,
		Committee:       keysSet.Committee(),
	}
}

var BaseInstance = func() *genesisspecqbft.Instance {
	return baseInstance(TestingShare(genesistestingutils.Testing4SharesSet()), genesistestingutils.Testing4SharesSet(), []byte{1, 2, 3, 4})
}

var SevenOperatorsInstance = func() *genesisspecqbft.Instance {
	return baseInstance(TestingShare(genesistestingutils.Testing7SharesSet()), genesistestingutils.Testing7SharesSet(), []byte{1, 2, 3, 4})
}

var TenOperatorsInstance = func() *genesisspecqbft.Instance {
	return baseInstance(TestingShare(genesistestingutils.Testing10SharesSet()), genesistestingutils.Testing10SharesSet(), []byte{1, 2, 3, 4})
}

var ThirteenOperatorsInstance = func() *genesisspecqbft.Instance {
	return baseInstance(TestingShare(genesistestingutils.Testing13SharesSet()), genesistestingutils.Testing13SharesSet(), []byte{1, 2, 3, 4})
}

var baseInstance = func(share *genesisspectypes.Share, keySet *genesistestingutils.TestKeySet, identifier []byte) *genesisspecqbft.Instance {
	ret := genesisspecqbft.NewInstance(genesistestingutils.TestingConfig(keySet), share, identifier, genesisspecqbft.FirstHeight)
	ret.StartValue = []byte{1, 2, 3, 4}
	return ret
}

func NewTestingQBFTController(
	identifier []byte,
	share *spectypes.Share,
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
