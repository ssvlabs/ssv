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
	sf.(Whitelist).Register(commons.AlanTopicFullName(1))
	require.True(t, sf.CanSubscribe(commons.AlanTopicFullName(1)))
	require.False(t, sf.CanSubscribe(commons.AlanTopicFullName(2)))
}
