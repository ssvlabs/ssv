package networkwrapper

import (
	"context"
	"fmt"
	"github.com/bloxapp/ssv/beacon"
	"github.com/bloxapp/ssv/ibft/proto"
	"github.com/bloxapp/ssv/network"
	forksv0 "github.com/bloxapp/ssv/network/forks/v0"
	p2pv1 "github.com/bloxapp/ssv/network/p2p_v1"
	v1_testing "github.com/bloxapp/ssv/network/p2p_v1/testing"
	"github.com/bloxapp/ssv/operator/forks"
	v0 "github.com/bloxapp/ssv/operator/forks/v0"
	v1 "github.com/bloxapp/ssv/operator/forks/v1"
	"github.com/bloxapp/ssv/utils/format"
	"github.com/bloxapp/ssv/utils/logex"
	"github.com/herumi/bls-eth-go-binary/bls"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"go.uber.org/zap/zaptest"
	"sync/atomic"
	"testing"
	"time"
)

func init() {
	logex.Build("test", zap.DebugLevel, nil)
}

func TestWrapper_Start(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	n := 4
	keys, err := v1_testing.CreateKeys(n)
	require.NoError(t, err)

	pks := []string{"b768cdc2b2e0a859052bf04d1cd66383c96d95096a5287d08151494ce709556ba39c1300fbb902a0e2ebb7c31dc4e400",
		"824b9024767a01b56790a72afb5f18bb0f97d5bddb946a7bd8dd35cc607c35a4d76be21f24f484d0d478b99dc63ed170"}
	validatorPk0 := &bls.PublicKey{}
	err = validatorPk0.DeserializeHexStr(pks[0])
	require.NoError(t, err)
	validatorPk1 := &bls.PublicKey{}
	err = validatorPk1.DeserializeHexStr(pks[0])
	require.NoError(t, err)

	nodes := make([]network.Network, n)
	for i := 0; i < n; i++ {
		logger := zaptest.NewLogger(t)
		//logger := zap.L()
		forker := forks.NewForker(forks.Config{
			Logger:     logger.With(zap.String("who", "forker")),
			Network:    "prater",
			ForkSlot:   100000000,
			BeforeFork: v0.New(),
			PostFork:   v1.New(),
		})
		forker.Start()
		nodes[i] = newNode(t, logger, fmt.Sprintf("node-%d", i), keys[i], forker)

		require.NoError(t, nodes[i].SubscribeToValidatorNetwork(validatorPk0))
		require.NoError(t, nodes[i].SubscribeToValidatorNetwork(validatorPk1))
	}
	<-time.After(time.Second * 2)

	recieved := make([]uint64, n)

	for i, node := range nodes {
		go func(node network.Network, i int) {
			in, done := node.ReceivedMsgChan()
			defer done()

			for ctx.Err() == nil {
				msg := <-in
				node.(*P2pNetwork).logger.Debug("got message", zap.Any("msg", msg))
				atomic.AddUint64(&recieved[i], 1)
			}
		}(node, i)
		idn := format.IdentifierFormat(validatorPk0.Serialize(), beacon.RoleTypeAttester.String())
		require.NoError(t, node.Broadcast(validatorPk0.Serialize(), &proto.SignedMessage{
			Message: &proto.Message{Lambda: []byte(idn), SeqNumber: uint64(i)},
		}))
	}
	<-time.After(time.Second * 2)
	for i := range nodes {
		require.Greater(t, atomic.LoadUint64(&recieved[i]), uint64(1))
	}
}

func newNode(t *testing.T, logger *zap.Logger, id string, keys v1_testing.NodeKeys, forker *forks.Forker) network.Network {
	cfg := &p2pv1.Config{
		NetworkPrivateKey: keys.NetKey,
		Logger:            logger.With(zap.String("id", id)),
		UserAgent:         forksv0.GenerateUserAgent(keys.OperatorKey),
		//OperatorPublicKey: &keys.OperatorKey.PublicKey,
		TCPPort:          v1_testing.RandomTCPPort(12001, 12999),
		MaxPeers:         10,
		MaxBatchResponse: 25,
		RequestTimeout:   time.Second * 5,
	}
	p2pNet, err := New(context.Background(), cfg, forker)
	require.NoError(t, err)
	require.NotNil(t, p2pNet)

	return p2pNet
}
