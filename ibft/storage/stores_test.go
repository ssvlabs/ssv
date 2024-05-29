package storage

import (
	"testing"

	specqbft "github.com/ssvlabs/ssv-spec/qbft"
	qbftstorage "github.com/ssvlabs/ssv/protocol/v2/qbft/storage"
	"github.com/ssvlabs/ssv/storage/basedb"
	"github.com/ssvlabs/ssv/storage/kv"

	"github.com/ssvlabs/ssv-spec/types"
	"github.com/stretchr/testify/require"

	"github.com/ssvlabs/ssv/logging"
)

func TestQBFTStores(t *testing.T) {
	logger := logging.TestLogger(t)

	qbftMap := NewStores()

	store, err := newTestIbftStorage(logger, "")
	require.NoError(t, err)
	qbftMap.Add(types.BNRoleAttester, store)
	qbftMap.Add(types.BNRoleProposer, store)

	require.NotNil(t, qbftMap.Get(types.BNRoleAttester))
	require.NotNil(t, qbftMap.Get(types.BNRoleProposer))

	db, err := kv.NewInMemory(logger.Named(logging.NameBadgerDBLog), basedb.Options{
		Reporting: true,
	})
	require.NoError(t, err)
	qbftMap = NewStoresFromRoles(db, types.BNRoleAttester, types.BNRoleProposer)

	require.NotNil(t, qbftMap.Get(types.BNRoleAttester))
	require.NotNil(t, qbftMap.Get(types.BNRoleProposer))

	id := []byte{1, 2, 3}

	qbftMap.Each(func(role types.BeaconRole, store qbftstorage.QBFTStore) error {
		return store.SaveInstance(&qbftstorage.StoredInstance{State: &specqbft.State{Height: 1, ID: id}})
	})

	instance, err := qbftMap.Get(types.BNRoleAttester).GetInstance(id, 1)
	require.NoError(t, err)
	require.NotNil(t, instance)
	require.Equal(t, specqbft.Height(1), instance.State.Height)
	require.Equal(t, id, instance.State.ID)
}
