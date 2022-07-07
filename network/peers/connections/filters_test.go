package connections

import (
	"github.com/bloxapp/ssv/network/records"
	"github.com/stretchr/testify/require"
	"testing"
)

func TestNetworkIDFilter(t *testing.T) {
	f := NetworkIDFilter("xxx")

	ok, err := f(&records.NodeInfo{
		NetworkID: "xxx",
	})
	require.NoError(t, err)
	require.True(t, ok)

	ok, err = f(&records.NodeInfo{
		NetworkID: "bbb",
	})
	require.Error(t, err)
	require.False(t, ok)
}
