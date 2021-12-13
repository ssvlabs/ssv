package p2p

import (
	"context"
	"crypto/ecdsa"
	"github.com/bloxapp/ssv/network"
	"github.com/bloxapp/ssv/utils"
	"github.com/bloxapp/ssv/utils/logex"
	"github.com/bloxapp/ssv/utils/threadsafe"
	core "github.com/libp2p/go-libp2p-core"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"testing"
	"time"
)

func testPrivKey(t *testing.T) *ecdsa.PrivateKey {
	ret, err := utils.ECDSAPrivateKey(logex.Build("test", zap.InfoLevel, nil), "")
	require.NoError(t, err)
	return ret
}

func testPeers(t *testing.T, logger *zap.Logger) (network.Network, network.Network) {
	// create 2 peers
	peer1, err := New(context.Background(), logger, &Config{
		DiscoveryType:     discoveryTypeMdns,
		Enr:               "enr:-LK4QMIAfHA47rJnVBaGeoHwXOrXcCNvUaxFiDEE2VPCxQ40cu_k2hZsGP6sX9xIQgiVnI72uxBBN7pOQCo5d9izhkcBh2F0dG5ldHOIAAAAAAAAAACEZXRoMpD1pf1CAAAAAP__________gmlkgnY0gmlwhH8AAAGJc2VjcDI1NmsxoQJu41tZ3K8fb60in7AarjEP_i2zv35My_XW_D_t6Y1fJ4N0Y3CCE4iDdWRwgg-g",
		NetworkPrivateKey: testPrivKey(t),
		UDPPort:           12000,
		TCPPort:           13000,
		MaxBatchResponse:  10,
		RequestTimeout:    time.Second * 1,
		Fork:              testFork(),
	})
	require.NoError(t, err)

	peer2, err := New(context.Background(), logger, &Config{
		DiscoveryType:     discoveryTypeMdns,
		Enr:               "enr:-LK4QMIAfHA47rJnVBaGeoHwXOrXcCNvUaxFiDEE2VPCxQ40cu_k2hZsGP6sX9xIQgiVnI72uxBBN7pOQCo5d9izhkcBh2F0dG5ldHOIAAAAAAAAAACEZXRoMpD1pf1CAAAAAP__________gmlkgnY0gmlwhH8AAAGJc2VjcDI1NmsxoQJu41tZ3K8fb60in7AarjEP_i2zv35My_XW_D_t6Y1fJ4N0Y3CCE4iDdWRwgg-g",
		NetworkPrivateKey: testPrivKey(t),
		UDPPort:           12001,
		TCPPort:           13001,
		MaxBatchResponse:  10,
		RequestTimeout:    time.Second * 1,
		Fork:              testFork(),
	})
	require.NoError(t, err)

	<-time.After(time.Millisecond * 1500) // important to let nodes reach each other

	return peer1, peer2
}

func TestSyncStream_ReadWithTimeout(t *testing.T) {
	logger := logex.Build("test", zap.DebugLevel, nil)
	peer1, peer2 := testPeers(t, logger)
	s, err := peer1.(*p2pNetwork).host.NewStream(context.Background(), peer2.(*p2pNetwork).host.ID(), legacyMsgStream)
	require.NoError(t, err)

	strm := NewSyncStream(s)

	byts, err := strm.ReadWithTimeout(time.Second)
	require.EqualError(t, err, "i/o deadline reached")
	require.Len(t, byts, 0)
}

func TestSyncStream_ReadWithoutTimeout(t *testing.T) {
	logger := logex.Build("test", zap.DebugLevel, nil)
	peer1, peer2 := testPeers(t, logger)

	readByts := threadsafe.Bool()
	peer2.(*p2pNetwork).host.SetStreamHandler(legacyMsgStream, func(stream core.Stream) {
		netSyncStream := NewSyncStream(stream)

		// read msg
		buf, err := netSyncStream.ReadWithTimeout(time.Millisecond * 100)
		require.NoError(t, err)

		require.Len(t, buf, 10)
		readByts.Set(true)
	})

	s, err := peer1.(*p2pNetwork).host.NewStream(context.Background(), peer2.(*p2pNetwork).host.ID(), legacyMsgStream)
	require.NoError(t, err)
	strm := NewSyncStream(s)
	err = strm.WriteWithTimeout(make([]byte, 10), time.Millisecond*100)
	require.NoError(t, err)
	require.NoError(t, strm.CloseWrite())

	time.Sleep(time.Millisecond * 300)
	require.True(t, readByts.Get())
}
