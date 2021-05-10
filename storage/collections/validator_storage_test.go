package collections

import (
	"github.com/bloxapp/ssv/fixtures"
	"github.com/bloxapp/ssv/ibft/proto"
	"github.com/bloxapp/ssv/utils/threshold"
	"github.com/herumi/bls-eth-go-binary/bls"
	"github.com/stretchr/testify/require"
	"testing"

	"github.com/bloxapp/ssv/storage/tests"
)

func TestValidatorSerialize(t *testing.T){

}

func TestValidatorDeserialize(t *testing.T){

}

func TestSaveAndGetStorage(t *testing.T) {
	validatorStorage := ValidatorStorage{
		prefix: []byte("validator-"),
		db:     &tests.TestImplementedDb{},
		logger: nil,
	}

	threshold.Init()
	pk := &bls.PublicKey{}
	require.NoError(t, pk.Deserialize(fixtures.RefPk))
	sk := &bls.SecretKey{}
	require.NoError(t, sk.Deserialize(fixtures.RefSplitShares[0]))

	ibftCommittee := map[uint64]*proto.Node{
		1: {
			IbftId: 1,
			Pk:     fixtures.RefSplitSharesPubKeys[0],
			Sk:     sk.Serialize(),
		},
		2: {
			IbftId: 2,
			Pk:     fixtures.RefSplitSharesPubKeys[1],
		},
		3: {
			IbftId: 3,
			Pk:     fixtures.RefSplitSharesPubKeys[2],
		},
		4: {
			IbftId: 4,
			Pk:     fixtures.RefSplitSharesPubKeys[3],
		},
	}

	validator := Validator{
		NodeID:    1,
		PubKey:    pk,
		ShareKey:  sk,
		Committee: ibftCommittee,
	}
	require.NoError(t, validatorStorage.SaveValidatorShare(&validator))
}

