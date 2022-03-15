package network_wrapper

import (
	"context"
	ssv_identity "github.com/bloxapp/ssv/identity"
	p2pv1 "github.com/bloxapp/ssv/network/p2p_v1"
	v0 "github.com/bloxapp/ssv/operator/forks/v0"
	"github.com/bloxapp/ssv/storage"
	"github.com/bloxapp/ssv/storage/basedb"
	"github.com/bloxapp/ssv/utils/logex"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"testing"
)

func init() {
	logex.Build("test", zap.DebugLevel, nil)
}

func TestForkV1_Encoding(t *testing.T) {
	fork := v0.New("prater")
	fork.Start()

	cfg := &p2pv1.Config{}

	options := basedb.Options{
		Type:   "badger-memory",
		Logger: zap.L(),
		Path:   "",
	}
	db, err := storage.GetStorageFactory(options)
	istore := ssv_identity.NewIdentityStore(db, logex.GetLogger())
	netPrivKey, err := istore.SetupNetworkKey("")
	require.NoError(t, err)

	cfg.NetworkPrivateKey = netPrivKey
	cfg.Fork = fork.NetworkForker()
	cfg.Logger = logex.GetLogger()

	p2pNet, err := New(context.Background(), cfg)
	require.NoError(t, err)
	require.NotNil(t, p2pNet)
}
