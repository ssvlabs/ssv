package testingutils

import (
	"github.com/bloxapp/ssv/spec/qbft"
	"github.com/bloxapp/ssv/spec/types"
)

var TestingConfig = func(keySet *TestKeySet) *qbft.Config {
	return &qbft.Config{
		Signer:    NewTestingKeyManager(),
		SigningPK: keySet.Shares[1].GetPublicKey().Serialize(),
		Domain:    types.PrimusTestnet,
		ValueCheck: func(data []byte) error {
			return nil
		},
		Storage: NewTestingStorage(),
		Network: NewTestingNetwork(),
	}
}

var testShare = func(keysSet *TestKeySet) *types.Share {
	return &types.Share{
		OperatorID:      1,
		ValidatorPubKey: keysSet.PK.Serialize(),
		SharePubKey:     keysSet.Shares[1].GetPublicKey().Serialize(),
		DomainType:      types.PrimusTestnet,
		Quorum:          keysSet.Threshold,
		PartialQuorum:   keysSet.PartialThreshold,
		Committee:       keysSet.Committee(),
	}
}

var BaseInstance = func() *qbft.Instance {
	return baseInstance(testShare(Testing4SharesSet()), Testing4SharesSet(), []byte{1, 2, 3, 4})
}

var SevenOperatorsInstance = func() *qbft.Instance {
	return baseInstance(testShare(Testing7SharesSet()), Testing7SharesSet(), []byte{1, 2, 3, 4})
}

var TenOperatorsInstance = func() *qbft.Instance {
	return baseInstance(testShare(Testing10SharesSet()), Testing10SharesSet(), []byte{1, 2, 3, 4})
}

var ThirteenOperatorsInstance = func() *qbft.Instance {
	return baseInstance(testShare(Testing13SharesSet()), Testing13SharesSet(), []byte{1, 2, 3, 4})
}

var baseInstance = func(share *types.Share, keySet *TestKeySet, identifier []byte) *qbft.Instance {
	return qbft.NewInstance(TestingConfig(keySet), share, identifier, qbft.FirstHeight)
}

func NewTestingQBFTController(identifier []byte, share *types.Share, valCheck qbft.ProposedValueCheck) *qbft.Controller {
	return qbft.NewController(
		identifier,
		share,
		types.PrimusTestnet,
		NewTestingKeyManager(),
		valCheck,
		NewTestingStorage(),
		NewTestingNetwork(),
	)
}
