package prysmgrpc

import (
	"context"
	"github.com/bloxapp/ssv/utils/logex"
	"github.com/herumi/bls-eth-go-binary/bls"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"testing"
)

var keys = []string{
	"3f5445c3b6cff05142f6038d54923f946dd18fff46acf0936f37f315fd70aeab",
	"63e46ac1d912ddd0d0e9dc4a4998552ed7ae8688ce3d9ebd1646c7cc12f471bb",
	"3e5ee64d234a5aebf8e70e45f3591f01387e1e33eab0f09861675c4c33f72767",
	"42b15fb8bf13e4eaee2771885b767510e3fdfb039c0542245099b0956078cfb0",
}

func TestEndToEndAggregation(t *testing.T) {

}

func TestPrysmGRPC_RolesAt(t *testing.T) {
	beaconClient, err := New(context.Background(), logex.Build("", zap.DebugLevel), "pyrmont", []byte("BloxStaking"), "eth2-4000-prysm-ext.stage.bloxinfra.com:80")
	require.NoError(t, err)
	require.NoError(t, bls.Init(bls.BLS12_381))
	for _, k := range keys{
		shareKey := &bls.SecretKey{}
		require.NoError(t, shareKey.SetHexString(k))
		_, err := beaconClient.IsAggregator(context.Background(), 1419179, 130, shareKey)
		require.NoError(t, err)
	}
}
