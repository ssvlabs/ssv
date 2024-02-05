package discovery

import (
	"context"
	"encoding/hex"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/bloxapp/ssv/logging"
	"github.com/bloxapp/ssv/network/commons"
)

func Test_NewBootnode(t *testing.T) {
	bnSk, err := commons.GenNetworkKey()
	require.NoError(t, err)
	isk, err := commons.ECDSAPrivToInterface(bnSk)
	require.NoError(t, err)
	b, err := isk.Raw()
	require.NoError(t, err)
	logger := logging.TestLogger(t)
	bn, err := NewBootnode(context.Background(), logger, &BootnodeOptions{
		PrivateKey: hex.EncodeToString(b),
		ExternalIP: "127.0.0.1",
		Port:       13001,
	})
	require.NoError(t, err)
	require.NotEmpty(t, bn.ENR)
	bn.Close()
}
