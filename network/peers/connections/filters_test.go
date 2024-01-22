package connections

import (
	"testing"

	"github.com/bloxapp/ssv/network/records"
	"github.com/stretchr/testify/require"
)

func TestNetworkIDFilter(t *testing.T) {
	f := NetworkIDFilter("xxx")

	err := f("", &records.SignedNodeInfo{
		NodeInfo: &records.NodeInfo{
			NetworkID: "xxx",
		},
	})
	require.NoError(t, err)

	err = f("", &records.SignedNodeInfo{
		NodeInfo: &records.NodeInfo{
			NetworkID: "bbb",
		},
	})
	require.Error(t, err)
}
