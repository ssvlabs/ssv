package p2p

import (
	"context"
	"crypto/ecdsa"
	"github.com/bloxapp/ssv/utils/logex"
	core "github.com/libp2p/go-libp2p-core"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"testing"
	"time"
)

func testPrivKey(t *testing.T) *ecdsa.PrivateKey {
	ret, err := privKey(false)
	require.NoError(t, err)
	return ret
}

func testStreams(t *testing.T, logger *zap.Logger) core.Stream {
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

	ret, err := peer1.(*p2pNetwork).host.NewStream(context.Background(), peer2.(*p2pNetwork).host.ID())
	require.NoError(t, err)
	return ret
}

func TestSyncStream_ReadWithTimeout(t *testing.T) {
	logger := logex.Build("test", zap.DebugLevel, nil)
	s := NewSyncStream(testStreams(t, logger))

	byts, err := s.ReadWithTimeout(time.Second)
	require.NoError(t, err)
	require.Len(t, byts, 10)
}
