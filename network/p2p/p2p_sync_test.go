package p2p

import (
	"context"
	"github.com/bloxapp/ssv/network"
	"github.com/libp2p/go-libp2p-core/peer"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap/zaptest"
	"testing"
	"time"
)

func TestSyncMessageBroadcastingTimeout(t *testing.T) {
	logger := zaptest.NewLogger(t)

	// create 2 peers
	peer1, err := New(context.Background(), logger, &Config{
		DiscoveryType:     "mdns",
		BootstrapNodeAddr: []string{"enr:-LK4QMIAfHA47rJnVBaGeoHwXOrXcCNvUaxFiDEE2VPCxQ40cu_k2hZsGP6sX9xIQgiVnI72uxBBN7pOQCo5d9izhkcBh2F0dG5ldHOIAAAAAAAAAACEZXRoMpD1pf1CAAAAAP__________gmlkgnY0gmlwhH8AAAGJc2VjcDI1NmsxoQJu41tZ3K8fb60in7AarjEP_i2zv35My_XW_D_t6Y1fJ4N0Y3CCE4iDdWRwgg-g"},
		UDPPort:           12000,
		TCPPort:           13000,
		MaxBatchResponse:  10,
		RequestTimeout:    time.Second * 1,
	})
	require.NoError(t, err)

	peer2, err := New(context.Background(), logger, &Config{
		DiscoveryType:     "mdns",
		BootstrapNodeAddr: []string{"enr:-LK4QMIAfHA47rJnVBaGeoHwXOrXcCNvUaxFiDEE2VPCxQ40cu_k2hZsGP6sX9xIQgiVnI72uxBBN7pOQCo5d9izhkcBh2F0dG5ldHOIAAAAAAAAAACEZXRoMpD1pf1CAAAAAP__________gmlkgnY0gmlwhH8AAAGJc2VjcDI1NmsxoQJu41tZ3K8fb60in7AarjEP_i2zv35My_XW_D_t6Y1fJ4N0Y3CCE4iDdWRwgg-g"},
		UDPPort:           12001,
		TCPPort:           13001,
		MaxBatchResponse:  10,
		RequestTimeout:    time.Second * 1,
	})
	require.NoError(t, err)

	// broadcast msg
	messageToBroadcast := &network.SyncMessage{
		SignedMessages: nil,
		Type:           network.Sync_GetHighestType,
	}

	time.Sleep(time.Millisecond * 1500) // important to let nodes reach each other
	peerID := peer.Encode(peer2.(*p2pNetwork).host.ID())
	res, err := peer1.GetHighestDecidedInstance(peerID, messageToBroadcast)
	require.EqualError(t, err, "no response for sync request")
	time.Sleep(time.Millisecond * 100)
	require.Nil(t, res)
}

func TestSyncMessageBroadcasting(t *testing.T) {
	logger := zaptest.NewLogger(t)

	// create 2 peers
	peer1, err := New(context.Background(), logger, &Config{
		DiscoveryType:     "mdns",
		BootstrapNodeAddr: []string{"enr:-LK4QMIAfHA47rJnVBaGeoHwXOrXcCNvUaxFiDEE2VPCxQ40cu_k2hZsGP6sX9xIQgiVnI72uxBBN7pOQCo5d9izhkcBh2F0dG5ldHOIAAAAAAAAAACEZXRoMpD1pf1CAAAAAP__________gmlkgnY0gmlwhH8AAAGJc2VjcDI1NmsxoQJu41tZ3K8fb60in7AarjEP_i2zv35My_XW_D_t6Y1fJ4N0Y3CCE4iDdWRwgg-g"},
		UDPPort:           12000,
		TCPPort:           13000,
		MaxBatchResponse:  10,
		RequestTimeout:    time.Second * 1,
	})
	require.NoError(t, err)

	peer2, err := New(context.Background(), logger, &Config{
		DiscoveryType:     "mdns",
		BootstrapNodeAddr: []string{"enr:-LK4QMIAfHA47rJnVBaGeoHwXOrXcCNvUaxFiDEE2VPCxQ40cu_k2hZsGP6sX9xIQgiVnI72uxBBN7pOQCo5d9izhkcBh2F0dG5ldHOIAAAAAAAAAACEZXRoMpD1pf1CAAAAAP__________gmlkgnY0gmlwhH8AAAGJc2VjcDI1NmsxoQJu41tZ3K8fb60in7AarjEP_i2zv35My_XW_D_t6Y1fJ4N0Y3CCE4iDdWRwgg-g"},
		UDPPort:           12001,
		TCPPort:           13001,
		MaxBatchResponse:  10,
		RequestTimeout:    time.Second * 1,
	})
	require.NoError(t, err)

	// set receivers
	peer2Chan := peer2.ReceivedSyncMsgChan()

	var receivedStream network.SyncStream
	go func() {
		msgFromPeer1 := <-peer2Chan
		require.IsType(t, network.SyncMessage{}, *msgFromPeer1.Msg)
		require.EqualValues(t, peer1.(*p2pNetwork).host.ID().String(), msgFromPeer1.Msg.FromPeerID)
		require.EqualValues(t, network.Sync_GetHighestType, msgFromPeer1.Msg.Type)

		receivedStream = msgFromPeer1.Stream

		messageToBroadcast := &network.SyncMessage{
			SignedMessages: nil,
			Type:           network.Sync_GetHighestType,
		}
		require.NoError(t, peer2.RespondToHighestDecidedInstance(msgFromPeer1.Stream, messageToBroadcast))
	}()

	// broadcast msg
	messageToBroadcast := &network.SyncMessage{
		SignedMessages: nil,
		Type:           network.Sync_GetHighestType,
	}

	time.Sleep(time.Millisecond * 500) // important to let nodes reach each other
	peerID := peer.Encode(peer2.(*p2pNetwork).host.ID())
	res, err := peer1.GetHighestDecidedInstance(peerID, messageToBroadcast)
	require.NoError(t, err)
	time.Sleep(time.Millisecond * 100)

	// verify
	require.NotNil(t, res)
	require.IsType(t, network.SyncMessage{}, *res)
	require.EqualValues(t, peer2.(*p2pNetwork).host.ID().String(), res.FromPeerID)
	require.EqualValues(t, network.Sync_GetHighestType, res.Type)

	// verify stream closed
	require.NotNil(t, receivedStream)
	_, err = receivedStream.Write([]byte{1})
	require.EqualError(t, err, "stream closed")
}
