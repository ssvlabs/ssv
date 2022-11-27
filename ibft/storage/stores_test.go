package storage

import (
	"github.com/bloxapp/ssv-spec/types"
	forksprotocol "github.com/bloxapp/ssv/protocol/forks"
	"github.com/bloxapp/ssv/utils/logex"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap/zapcore"
	"testing"
)

func init() {
	logex.Build("", zapcore.DebugLevel, &logex.EncodingConfig{})
}

func TestMap(t *testing.T) {
	qbftMap := &QBFTStores{}

	store, err := newTestIbftStorage(logex.GetLogger(), "", forksprotocol.GenesisForkVersion)
	require.NoError(t, err)
	qbftMap.Add(types.BNRoleAttester, store)
	qbftMap.Add(types.BNRoleProposer, store)

	require.NotNil(t, qbftMap.Get(types.BNRoleAttester))
	require.NotNil(t, qbftMap.Get(types.BNRoleProposer))
}
