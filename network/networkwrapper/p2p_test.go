package networkwrapper

import (
	"context"
	ssv_identity "github.com/bloxapp/ssv/identity"
	p2pv1 "github.com/bloxapp/ssv/network/p2p_v1"
	"github.com/bloxapp/ssv/operator/forks"
	v0 "github.com/bloxapp/ssv/operator/forks/v0"
	v1 "github.com/bloxapp/ssv/operator/forks/v1"
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

func TestWrapper_Start(t *testing.T) {
	forker := forks.NewForker(forks.Config{
		Logger:     logex.GetLogger(),
		Network:    "prater",
		ForkSlot:   100000000,
		BeforeFork: v0.New(),
		PostFork:   v1.New(),
	})
	forker.Start()

	cfg := &p2pv1.Config{}

	options := basedb.Options{
		Type:   "badger-memory",
		Logger: zap.L(),
		Path:   "",
	}
	db, err := storage.GetStorageFactory(options)
	require.NoError(t, err)
	istore := ssv_identity.NewIdentityStore(db, logex.GetLogger())
	netPrivKey, err := istore.SetupNetworkKey("")
	require.NoError(t, err)

	cfg.NetworkPrivateKey = netPrivKey
	cfg.Fork = forker
	cfg.Logger = logex.GetLogger()

	p2pNet, err := New(context.Background(), cfg)
	require.NoError(t, err)
	require.NotNil(t, p2pNet)
}
