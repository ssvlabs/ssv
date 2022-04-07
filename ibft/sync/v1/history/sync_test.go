package history

import (
	"context"
	"fmt"
	p2p "github.com/bloxapp/ssv/network/p2p"
	forksprotocol "github.com/bloxapp/ssv/protocol/forks"
	"github.com/bloxapp/ssv/protocol/v1/blockchain/beacon"
	"github.com/bloxapp/ssv/protocol/v1/message"
	"github.com/bloxapp/ssv/protocol/v1/qbft/validation"
	"github.com/bloxapp/ssv/utils/threshold"
	"github.com/herumi/bls-eth-go-binary/bls"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"testing"
	"time"
)

func TestHistory_SyncDecided(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	nShares := 2
	nNodes := 4
	loggerFactory := func(who string) *zap.Logger {
		//logger := zaptest.NewLogger(t).With(zap.String("who", who))
		logger := zap.L().With(zap.String("who", who))
		return logger
	}
	ln, err := p2p.CreateAndStartLocalNet(ctx, loggerFactory, forksprotocol.V1ForkVersion, nNodes, nNodes/2, false)
	require.NoError(t, err)
	histories := make([]*history, 0)
	for i, node := range ln.Nodes {
		store := newDecidedStoreMock()
		// TODO: add data
		valPipeline := validation.WrapFunc(fmt.Sprintf("history_sync-validation-%d", i), func(signedMessage *message.SignedMessage) error {
			return nil
		})
		h := New(loggerFactory(fmt.Sprintf("history_sync-%d", i)), store, node, valPipeline)
		histories = append(histories, h.(*history))
	}
	require.Len(t, histories, len(ln.Nodes))

	// create share and subscribes to channels
	keys := createSharesKeys(nShares)
	for _, k := range keys {
		pk := k.Serialize()
		for _, node := range ln.Nodes {
			require.NoError(t, node.Subscribe(pk))
		}
	}

	<-time.After(time.Second * 2)

	// performs syncs from some node
	for _, k := range keys {
		pk := k.Serialize()
		idn := message.NewIdentifier(pk, beacon.RoleTypeAttester)

		synced, err := histories[2].SyncDecided(ctx, idn, false)
		require.NoError(t, err)
		require.True(t, synced)
	}

	// TODO: validate sync
}


func createSharesKeys(n int) []*bls.SecretKey {
	threshold.Init()

	var res []*bls.SecretKey
	for i := 0; i < n; i++ {
		sk := bls.SecretKey{}
		sk.SetByCSPRNG()
		res = append(res, &sk)
		fmt.Printf("\"%s\",", sk.GetPublicKey().SerializeToHexStr())
	}
	return res
}
