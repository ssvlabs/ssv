package p2p

import (
	"context"
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

	peer1, err := New(context.Background(), logger, &Config{
		DiscoveryType: discoveryTypeMdns,
		Enr:           "enr:-LK4QMIAfHA47rJnVBaGeoHwXOrXcCNvUaxFiDEE2VPCxQ40cu_k2hZsGP6sX9xIQgiVnI72uxBBN7pOQCo5d9izhkcBh2F0dG5ldHOIAAAAAAAAAACEZXRoMpD1pf1CAAAAAP__________gmlkgnY0gmlwhH8AAAGJc2VjcDI1NmsxoQJu41tZ3K8fb60in7AarjEP_i2zv35My_XW_D_t6Y1fJ4N0Y3CCE4iDdWRwgg-g",
		UDPPort:       12000,
		TCPPort:       13000,
		Fork:          testFork(),
	})
	require.NoError(t, err)

	peer2, err := New(context.Background(), logger, &Config{
		DiscoveryType: discoveryTypeMdns,
		Enr:           "enr:-LK4QMIAfHA47rJnVBaGeoHwXOrXcCNvUaxFiDEE2VPCxQ40cu_k2hZsGP6sX9xIQgiVnI72uxBBN7pOQCo5d9izhkcBh2F0dG5ldHOIAAAAAAAAAACEZXRoMpD1pf1CAAAAAP__________gmlkgnY0gmlwhH8AAAGJc2VjcDI1NmsxoQJu41tZ3K8fb60in7AarjEP_i2zv35My_XW_D_t6Y1fJ4N0Y3CCE4iDdWRwgg-g",
		UDPPort:       12001,
		TCPPort:       13001,
		Fork:          testFork(),
	})
	require.NoError(t, err)

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

	peer1Chan := peer1.ReceivedMsgChan()
	peer2Chan := peer2.ReceivedMsgChan()

	time.Sleep(time.Second)

	err = peer1.Broadcast(pk.Serialize(), messageToBroadcast)
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
