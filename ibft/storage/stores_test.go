package storage

import (
	"github.com/bloxapp/ssv/logging"
	"testing"

	"github.com/bloxapp/ssv-spec/types"
	forksprotocol "github.com/bloxapp/ssv/protocol/forks"
	"github.com/stretchr/testify/require"
)

func TestQBFTStores(t *testing.T) {
	qbftMap := NewStores()

	store, err := newTestIbftStorage(logging.TestLogger(t), "", forksprotocol.GenesisForkVersion)
	require.NoError(t, err)
	qbftMap.Add(types.BNRoleAttester, store)
	qbftMap.Add(types.BNRoleProposer, store)

	require.NotNil(t, qbftMap.Get(types.BNRoleAttester))
	require.NotNil(t, qbftMap.Get(types.BNRoleProposer))
}
