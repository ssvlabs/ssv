package topics

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/bloxapp/ssv/logging"
	"github.com/bloxapp/ssv/network/commons"
)

func TestSubFilter(t *testing.T) {
	l := logging.TestLogger(t)
	sf := newSubFilter(l, 2)

	require.False(t, sf.CanSubscribe("xxx"))
	require.False(t, sf.CanSubscribe(commons.GetTopicFullName("xxx")))
	sf.(Whitelist).Register(commons.GetTopicFullName("1"))
	require.True(t, sf.CanSubscribe(commons.GetTopicFullName("1")))
	require.False(t, sf.CanSubscribe(commons.GetTopicFullName("2")))
}
