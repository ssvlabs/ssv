package p2p

import (
	"context"
	"github.com/herumi/bls-eth-go-binary/bls"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"go.uber.org/zap/zaptest"

	"github.com/bloxapp/ssv/ibft/proto"
)

func TestP2PNetworker(t *testing.T) {
	logger := zaptest.NewLogger(t)

	peer1, err := New(context.Background(), logger, &Config{
		DiscoveryType:     "mdns",
		BootstrapNodeAddr: []string{"enr:-LK4QMIAfHA47rJnVBaGeoHwXOrXcCNvUaxFiDEE2VPCxQ40cu_k2hZsGP6sX9xIQgiVnI72uxBBN7pOQCo5d9izhkcBh2F0dG5ldHOIAAAAAAAAAACEZXRoMpD1pf1CAAAAAP__________gmlkgnY0gmlwhH8AAAGJc2VjcDI1NmsxoQJu41tZ3K8fb60in7AarjEP_i2zv35My_XW_D_t6Y1fJ4N0Y3CCE4iDdWRwgg-g"},
		UDPPort:           12000,
		TCPPort:           13000,
	})
	require.NoError(t, err)

	peer2, err := New(context.Background(), logger, &Config{
		DiscoveryType:     "mdns",
		BootstrapNodeAddr: []string{"enr:-LK4QMIAfHA47rJnVBaGeoHwXOrXcCNvUaxFiDEE2VPCxQ40cu_k2hZsGP6sX9xIQgiVnI72uxBBN7pOQCo5d9izhkcBh2F0dG5ldHOIAAAAAAAAAACEZXRoMpD1pf1CAAAAAP__________gmlkgnY0gmlwhH8AAAGJc2VjcDI1NmsxoQJu41tZ3K8fb60in7AarjEP_i2zv35My_XW_D_t6Y1fJ4N0Y3CCE4iDdWRwgg-g"},
		UDPPort:           12001,
		TCPPort:           13001,
	})
	require.NoError(t, err)

	pk := &bls.PublicKey{}
	require.NoError(t, pk.Deserialize(refPk))
	require.NoError(t, peer1.SubscribeToValidatorNetwork(pk))
	require.NoError(t, peer2.SubscribeToValidatorNetwork(pk))

	lambda := []byte("test-lambda")
	messageToBroadcast := &proto.SignedMessage{
		Message: &proto.Message{
			Type:        proto.RoundState_PrePrepare,
			Round:       1,
			Lambda:      lambda,
			Value:       []byte("test-value"),
			ValidatorPk: refPk,
		},
	}

	time.Sleep(time.Second)

	peer1Chan := peer1.ReceivedMsgChan()
	peer2Chan := peer2.ReceivedMsgChan()

	time.Sleep(time.Second)

	err = peer1.Broadcast(messageToBroadcast)
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
