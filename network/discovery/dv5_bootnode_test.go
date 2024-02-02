package discovery

import (
	"context"
	"encoding/hex"
	"testing"

	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"

	"github.com/bloxapp/ssv/network/commons"
)

func Test_NewBootnode(t *testing.T) {
	bnSk, err := commons.GenNetworkKey()
	require.NoError(t, err)
	isk, err := commons.ECDSAPrivToInterface(bnSk)
	require.NoError(t, err)
	b, err := isk.Raw()
	require.NoError(t, err)
	logger := zap.New(zapcore.NewNopCore(), zap.WithFatalHook(zapcore.WriteThenPanic))
	bn, err := NewBootnode(context.Background(), logger, &BootnodeOptions{
		PrivateKey: hex.EncodeToString(b),
		ExternalIP: "127.0.0.1",
		Port:       13001,
	})
	require.NoError(t, err)
	bn.Close()
}
