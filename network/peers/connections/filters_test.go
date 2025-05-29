package connections

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ssvlabs/ssv/network/records"
)

func TestNetworkIDFilter(t *testing.T) {
	f := NetworkIDFilter("xxx")

	err := f("", &records.NodeInfo{
		NetworkID: "xxx",
	})
	require.NoError(t, err)

	err = f("", &records.NodeInfo{
		NetworkID: "bbb",
	})
	require.Error(t, err)
}
