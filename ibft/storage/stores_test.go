package storage

import (
	"testing"

	specqbft "github.com/bloxapp/ssv-spec/qbft"
	spectypes "github.com/bloxapp/ssv-spec/types"
	"github.com/bloxapp/ssv/logging"
	qbftstorage "github.com/bloxapp/ssv/protocol/v2/qbft/storage"
	"github.com/bloxapp/ssv/storage/basedb"
	"github.com/bloxapp/ssv/storage/kv"
	"github.com/stretchr/testify/require"
)

func TestQBFTStores(t *testing.T) {
	logger := logging.TestLogger(t)

	qbftMap := NewStores()

	store, err := newTestIbftStorage(logger, "")
	require.NoError(t, err)
	qbftMap.Add(spectypes.RoleCommittee, store)
	qbftMap.Add(spectypes.RoleCommittee, store)

	require.NotNil(t, qbftMap.Get(spectypes.RoleCommittee))
	require.NotNil(t, qbftMap.Get(spectypes.RoleCommittee))

	db, err := kv.NewInMemory(logger.Named(logging.NameBadgerDBLog), basedb.Options{
		Reporting: true,
	})
	require.NoError(t, err)
	qbftMap = NewStoresFromRoles(db, spectypes.RoleCommittee, spectypes.RoleProposer)

	require.NotNil(t, qbftMap.Get(spectypes.RoleCommittee))
	require.NotNil(t, qbftMap.Get(spectypes.RoleCommittee))

	id := []byte{1, 2, 3}

	qbftMap.Each(func(role spectypes.RunnerRole, store qbftstorage.QBFTStore) error {
		return store.SaveInstance(&qbftstorage.StoredInstance{State: &specqbft.State{Height: 1, ID: id}})
	})

	instance, err := qbftMap.Get(spectypes.RoleCommittee).GetInstance(id, 1)
	require.NoError(t, err)
	require.NotNil(t, instance)
	require.Equal(t, specqbft.Height(1), instance.State.Height)
	require.Equal(t, id, instance.State.ID)
}
