package storage

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ssvlabs/ssv/logging"
	genesisqbftstorage "github.com/ssvlabs/ssv/protocol/genesis/qbft/storage"
	genesistypes "github.com/ssvlabs/ssv/protocol/genesis/types"
	"github.com/ssvlabs/ssv/storage/basedb"
	"github.com/ssvlabs/ssv/storage/kv"

	genesisspecqbft "github.com/ssvlabs/ssv-spec-pre-cc/qbft"
	genesisspectypes "github.com/ssvlabs/ssv-spec-pre-cc/types"
)

func TestQBFTStores(t *testing.T) {
	logger := logging.TestLogger(t)

	qbftMap := NewStores()

	store, err := newTestIbftStorage(logger, "")
	require.NoError(t, err)
	qbftMap.Add(genesisspectypes.BNRoleAttester, store)
	qbftMap.Add(genesisspectypes.BNRoleProposer, store)

	require.NotNil(t, qbftMap.Get(genesisspectypes.BNRoleAttester))
	require.NotNil(t, qbftMap.Get(genesisspectypes.BNRoleProposer))

	db, err := kv.NewInMemory(logger.Named(logging.NameBadgerDBLog), basedb.Options{
		Reporting: true,
	})
	require.NoError(t, err)
	qbftMap = NewStoresFromRoles(db, genesisspectypes.BNRoleAttester, genesisspectypes.BNRoleProposer)

	require.NotNil(t, qbftMap.Get(genesisspectypes.BNRoleAttester))
	require.NotNil(t, qbftMap.Get(genesisspectypes.BNRoleProposer))

	id := []byte{1, 2, 3}

	qbftMap.Each(func(role genesisspectypes.BeaconRole, store genesisqbftstorage.QBFTStore) error {
		return store.SaveInstance(&genesisqbftstorage.StoredInstance{State: &genesistypes.State{Height: 1, ID: id}})
	})

	instance, err := qbftMap.Get(genesisspectypes.BNRoleAttester).GetInstance(id, 1)
	require.NoError(t, err)
	require.NotNil(t, instance)
	require.Equal(t, genesisspecqbft.Height(1), instance.State.Height)
	require.Equal(t, id, instance.State.ID)
}
