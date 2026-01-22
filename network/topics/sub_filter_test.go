package topics

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ssvlabs/ssv/network/commons"
	"github.com/ssvlabs/ssv/observability/log"
)

func TestSubFilter(t *testing.T) {
	l := log.TestLogger(t)
	sf := newSubFilter(l, 2)

	require.False(t, sf.CanSubscribe("xxx"))
	require.False(t, sf.CanSubscribe("ssv.v2.xxx"))
	sf.(Whitelist).Register(commons.Subnet(1).AlanTopic())
	require.True(t, sf.CanSubscribe(commons.Subnet(1).AlanTopic()))
	require.False(t, sf.CanSubscribe(commons.Subnet(2).AlanTopic()))
}
