package p2p

import (
	"context"
	"crypto/ecdsa"
	"github.com/bloxapp/ssv/network"
	v0 "github.com/bloxapp/ssv/network/forks/v0"
	"github.com/bloxapp/ssv/utils"
	"github.com/bloxapp/ssv/utils/logex"
	"github.com/libp2p/go-libp2p-core/host"
	"github.com/libp2p/go-libp2p-core/peer"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"testing"
	"time"
)

func TestSyncMessageBroadcastingTimeout(t *testing.T) {
	logger := logex.Build("test", zap.DebugLevel, nil)

	peer1, peer2 := testPeers(t, logger)

	// broadcast msg
	messageToBroadcast := &network.SyncMessage{
		SignedMessages: nil,
		Type:           network.Sync_GetHighestType,
	}

	peerID := peer.Encode(peer2.(*p2pNetwork).host.ID())
	res, err := peer1.GetHighestDecidedInstance(peerID, messageToBroadcast)
	require.EqualError(t, err, "failed to make sync request: could not read stream msg: i/o deadline reached")
	time.Sleep(time.Millisecond * 100)
	require.Nil(t, res)
}

func TestSyncMessageBroadcasting(t *testing.T) {
	logger := logex.Build("test", zapcore.InfoLevel, nil)

	peer1, peer2 := testPeers(t, logger)

	// set receivers
	peer2Chan, done := peer2.ReceivedSyncMsgChan()
	defer done()

	go func() {
		msgFromPeer1 := <-peer2Chan
		require.EqualValues(t, peer1.(*p2pNetwork).host.ID().String(), msgFromPeer1.Msg.FromPeerID)
		require.EqualValues(t, network.Sync_GetHighestType, msgFromPeer1.Msg.Type)

		messageToBroadcast := &network.SyncMessage{
			SignedMessages: nil,
			Type:           network.Sync_GetHighestType,
		}
		require.NoError(t, peer2.RespondSyncMsg(msgFromPeer1.StreamID, messageToBroadcast))
	}()

	// broadcast msg
	messageToBroadcast := &network.SyncMessage{
		SignedMessages: nil,
		Type:           network.Sync_GetHighestType,
	}

	time.Sleep(time.Millisecond * 800) // sleep to let nodes reach each other

	peerID := peer.Encode(peer2.(*p2pNetwork).host.ID())
	res, err := peer1.GetHighestDecidedInstance(peerID, messageToBroadcast)
	require.NoError(t, err)
	time.Sleep(time.Millisecond * 100)

	// verify
	require.NotNil(t, res)
	require.EqualValues(t, peer2.(*p2pNetwork).host.ID().String(), res.FromPeerID)
	require.EqualValues(t, network.Sync_GetHighestType, res.Type)
}

func testPrivKey(t *testing.T) *ecdsa.PrivateKey {
	ret, err := utils.ECDSAPrivateKey(logex.Build("test", zap.InfoLevel, nil), "")
	require.NoError(t, err)
	return ret
}

func testPeers(t *testing.T, logger *zap.Logger) (network.Network, network.Network) {
	// create 2 peers
	peer1, _, err := testNetwork(context.Background(), logger, testPrivKey(t))
	require.NoError(t, err)

	peer2, _, err := testNetwork(context.Background(), logger, testPrivKey(t))
	require.NoError(t, err)

	<-time.After(time.Millisecond * 1500) // important to let nodes reach each other

	return peer1, peer2
}

// testNetwork creates a new network for tests
func testNetwork(ctx context.Context, logger *zap.Logger, netKey *ecdsa.PrivateKey) (network.Network, host.Host, error) {
	n, err := New(ctx, logger, &Config{
		DiscoveryType:     discoveryTypeMdns,
		Enr:               "enr:-LK4QMIAfHA47rJnVBaGeoHwXOrXcCNvUaxFiDEE2VPCxQ40cu_k2hZsGP6sX9xIQgiVnI72uxBBN7pOQCo5d9izhkcBh2F0dG5ldHOIAAAAAAAAAACEZXRoMpD1pf1CAAAAAP__________gmlkgnY0gmlwhH8AAAGJc2VjcDI1NmsxoQJu41tZ3K8fb60in7AarjEP_i2zv35My_XW_D_t6Y1fJ4N0Y3CCE4iDdWRwgg-g",
		NetworkPrivateKey: netKey,
		UDPPort:           12000,
		TCPPort:           13000,
		MaxBatchResponse:  10,
		RequestTimeout:    time.Second * 1,
		Fork:              &v0.ForkV0{},
	})
	if err != nil {
		return nil, nil, err
	}
	return n, n.(*p2pNetwork).host, nil
}
