package tests

import (
	"github.com/bloxapp/ssv/fixtures"
	"github.com/bloxapp/ssv/ibft/proto"
	"github.com/bloxapp/ssv/storage"
	"github.com/bloxapp/ssv/storage/collections"
	"github.com/bloxapp/ssv/utils/threshold"
	"github.com/herumi/bls-eth-go-binary/bls"
	"github.com/stretchr/testify/require"
	"testing"
)

type TestImplementedDb struct {
	s string
}

func (t *TestImplementedDb) Set(prefix []byte, key []byte, value []byte) error {
	return nil
}

func (t *TestImplementedDb) Get(prefix []byte, key []byte) (storage.Obj, error) {
	return storage.Obj{
		Key:   key,
		Value: nil,
	}, nil
}

func (t *TestImplementedDb) GetAllByCollection(prefix []byte) ([]storage.Obj, error) {
	return []storage.Obj{
		{
			Key:   nil,
			Value: nil,
		},
	}, nil
}

// TestSKs returns reference test shares for SSV
func TestValidators(t *testing.T) []collections.Validator {
	return []collections.Validator{
		generateValidator(t, 1),
		generateValidator(t, 2),
	}
}

func generateValidator(t *testing.T, nodeId uint64) collections.Validator {
	threshold.Init()
	pk := &bls.PublicKey{}
	require.NoError(t, pk.Deserialize(fixtures.RefPk))
	sk := &bls.SecretKey{}
	require.NoError(t, sk.Deserialize(fixtures.RefSplitShares[nodeId -1]))

	ibftCommittee := map[uint64]*proto.Node{
		1: {
			IbftId: 1,
			Pk:     fixtures.RefSplitSharesPubKeys[0],
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
	ibftCommittee[nodeId].Sk = sk.Serialize()

	return collections.Validator{
		NodeID:    nodeId,
		PubKey:    pk,
		ShareKey:  sk,
		Committee: ibftCommittee,
	}
}
