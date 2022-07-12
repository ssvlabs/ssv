package topics

import (
	forksfactory "github.com/bloxapp/ssv/network/forks/factory"
	"github.com/bloxapp/ssv/network/forks/genesis"
	forksprotocol "github.com/bloxapp/ssv/protocol/forks"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"testing"
)

func TestSubFilter(t *testing.T) {
	f := forksfactory.NewFork(forksprotocol.GenesisForkVersion)
	l := zap.L()
	sf := newSubFilter(l, f, 2)

	require.False(t, sf.CanSubscribe("xxx"))
	require.False(t, sf.CanSubscribe(genesis.New().GetTopicFullName("xxx")))
	require.True(t, sf.CanSubscribe(f.GetTopicFullName("1")))
}
