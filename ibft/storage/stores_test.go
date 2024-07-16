package storage

import (
	"github.com/ssvlabs/ssv/exporter/convert"
	"testing"

	specqbft "github.com/ssvlabs/ssv-spec/qbft"
	"github.com/ssvlabs/ssv/logging"
	qbftstorage "github.com/ssvlabs/ssv/protocol/v2/qbft/storage"
	"github.com/ssvlabs/ssv/storage/basedb"
	"github.com/ssvlabs/ssv/storage/kv"
	"github.com/stretchr/testify/require"
)

func TestQBFTStores(t *testing.T) {
	logger := logging.TestLogger(t)

	qbftMap := NewStores()

	store, err := newTestIbftStorage(logger, "")
	require.NoError(t, err)
	qbftMap.Add(convert.RoleCommittee, store)
	qbftMap.Add(convert.RoleCommittee, store)

	require.NotNil(t, qbftMap.Get(convert.RoleCommittee))
	require.NotNil(t, qbftMap.Get(convert.RoleCommittee))

	db, err := kv.NewInMemory(logger.Named(logging.NameBadgerDBLog), basedb.Options{
		Reporting: true,
	})
	require.NoError(t, err)
	qbftMap = NewStoresFromRoles(db, convert.RoleCommittee, convert.RoleProposer)

	require.NotNil(t, qbftMap.Get(convert.RoleCommittee))
	require.NotNil(t, qbftMap.Get(convert.RoleCommittee))

	id := []byte{1, 2, 3}

	err = qbftMap.Each(func(role convert.RunnerRole, store qbftstorage.QBFTStore) error {
		return store.SaveInstance(&qbftstorage.StoredInstance{State: &specqbft.State{Height: 1, ID: id}})
	})
	require.NoError(t, err)

	instance, err := qbftMap.Get(convert.RoleCommittee).GetInstance(id, 1)
	require.NoError(t, err)
	require.NotNil(t, instance)
	require.Equal(t, specqbft.Height(1), instance.State.Height)
	require.Equal(t, id, instance.State.ID)
}
