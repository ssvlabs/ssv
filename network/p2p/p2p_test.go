package p2p

import (
	"github.com/bloxapp/ssv/fixtures"
	"github.com/bloxapp/ssv/utils/commons"
	"github.com/bloxapp/ssv/utils/rsaencryption"
	"github.com/bloxapp/ssv/utils/threshold"
	"github.com/herumi/bls-eth-go-binary/bls"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"go.uber.org/zap/zaptest"

	"github.com/bloxapp/ssv/ibft/proto"
)

func TestP2PNetworker(t *testing.T) {
	threshold.Init()
	logger := zaptest.NewLogger(t)
	threshold.Init()

	peer1, peer2 := testPeers(t, logger)

	pk := &bls.PublicKey{}
	require.NoError(t, pk.Deserialize(fixtures.RefPk))
	require.NoError(t, peer1.SubscribeToValidatorNetwork(pk))
	require.NoError(t, peer2.SubscribeToValidatorNetwork(pk))

	lambda := []byte("test-lambda")
	messageToBroadcast := &proto.SignedMessage{
		Message: &proto.Message{
			Type:   proto.RoundState_PrePrepare,
			Round:  1,
			Lambda: lambda,
			Value:  []byte("test-value"),
		},
	}

	time.Sleep(time.Second)

	peer1Chan, done1 := peer1.ReceivedMsgChan()
	defer done1()
	peer2Chan, done2 := peer2.ReceivedMsgChan()
	defer done2()

	time.Sleep(time.Second)

	err := peer1.Broadcast(pk.Serialize(), messageToBroadcast)
	require.NoError(t, err)

	time.Sleep(time.Second)

	t.Run("peer 1 receives message", func(t *testing.T) {
		msgFromPeer1 := <-peer1Chan
		require.Equal(t, messageToBroadcast, msgFromPeer1)
	})

	t.Run("peer 2 receives message", func(t *testing.T) {
		msgFromPeer2 := <-peer2Chan
		require.Equal(t, messageToBroadcast, msgFromPeer2)
	})
}

func TestP2pNetwork_GetUserAgent(t *testing.T) {
	commons.SetBuildData("ssvtest", "v0.x.x")

	t.Run("with operator key", func(t *testing.T) {
		_, skBytes, err := rsaencryption.GenerateKeys()
		require.NoError(t, err)
		require.NotNil(t, skBytes)
		sk, err := rsaencryption.ConvertPemToPrivateKey(string(skBytes))
		require.NoError(t, err)
		require.NotNil(t, sk)
		n := p2pNetwork{operatorPrivKey: sk}
		ua := n.getUserAgent()
		parts := strings.Split(ua, ":")
		require.Equal(t, "ssvtest", parts[0])
		require.Equal(t, "v0.x.x", parts[1])
		pk, err := rsaencryption.ExtractPublicKey(sk)
		require.NoError(t, err)
		require.Equal(t, pubKeyHash(pk), parts[2])
	})

	t.Run("without operator key", func(t *testing.T) {
		n := p2pNetwork{}
		require.Equal(t, "ssvtest:v0.x.x", n.getUserAgent())
	})
}
