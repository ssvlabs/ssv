package topics

import (
	"testing"

	forksfactory "github.com/bloxapp/ssv/network/forks/factory"
	forksprotocol "github.com/bloxapp/ssv/protocol/forks"
	"github.com/bloxapp/ssv/utils/logex"
	"github.com/stretchr/testify/require"
)

func TestSubFilter(t *testing.T) {
	f := forksfactory.NewFork(forksprotocol.GenesisForkVersion)
	l := logex.TestLogger(t)
	sf := newSubFilter(l, f, 2)

	require.False(t, sf.CanSubscribe("xxx"))
	require.False(t, sf.CanSubscribe(f.GetTopicFullName("xxx")))
	sf.(Whitelist).Register(f.GetTopicFullName("1"))
	require.True(t, sf.CanSubscribe(f.GetTopicFullName("1")))
	require.False(t, sf.CanSubscribe(f.GetTopicFullName("2")))
}
