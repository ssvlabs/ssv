package p2pv1

import (
	"context"
	crand "crypto/rand"
	"math/rand"
	"testing"
	"time"

	"github.com/libp2p/go-libp2p"
	libp2pcrypto "github.com/libp2p/go-libp2p/core/crypto"
	p2pnet "github.com/libp2p/go-libp2p/core/network"
	"github.com/libp2p/go-libp2p/core/peer"
	libp2pdiscbackoff "github.com/libp2p/go-libp2p/p2p/discovery/backoff"
	ma "github.com/multiformats/go-multiaddr"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"go.uber.org/zap/zaptest/observer"
)

func TestBackoffConnector(t *testing.T) {
	hostA, err := libp2p.New(libp2p.ListenAddrStrings("/ip4/127.0.0.1/tcp/0"))
	require.NoError(t, err)
	defer hostA.Close()

	hostB, err := libp2p.New(libp2p.ListenAddrStrings("/ip4/127.0.0.1/tcp/0"))
	require.NoError(t, err)
	defer hostB.Close()

	core, logs := observer.New(zapcore.DebugLevel)
	logger := zap.New(core)

	backoffFactory := libp2pdiscbackoff.NewExponentialDecorrelatedJitter(
		backoffLow,
		backoffHigh,
		backoffExponentBase,
		rand.NewSource(1),
	)
	connector, err := newBackoffConnector(logger, hostA, backoffConnectorCacheSize, 5*time.Second, backoffFactory)
	require.NoError(t, err)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	peerCh := make(chan peer.AddrInfo)
	go connector.Connect(ctx, peerCh)

	// A reachable peer gets connected.
	peerCh <- peer.AddrInfo{ID: hostB.ID(), Addrs: hostB.Addrs()}
	require.Eventually(t, func() bool {
		return hostA.Network().Connectedness(hostB.ID()) == p2pnet.Connected
	}, 5*time.Second, 50*time.Millisecond)
	require.Equal(t, 0, logs.FilterMessage("could not connect to discovered peer").Len())

	// An unreachable peer produces a dial-failure log.
	unreachablePriv, _, err := libp2pcrypto.GenerateEd25519Key(crand.Reader)
	require.NoError(t, err)
	unreachableID, err := peer.IDFromPrivateKey(unreachablePriv)
	require.NoError(t, err)
	unreachableAddr, err := ma.NewMultiaddr("/ip4/127.0.0.1/tcp/1")
	require.NoError(t, err)

	unreachable := peer.AddrInfo{ID: unreachableID, Addrs: []ma.Multiaddr{unreachableAddr}}
	peerCh <- unreachable
	require.Eventually(t, func() bool {
		return logs.FilterMessage("could not connect to discovered peer").Len() == 1
	}, 5*time.Second, 50*time.Millisecond)

	// Re-proposing the same peer within its backoff period must not trigger another dial.
	peerCh <- unreachable
	require.Never(t, func() bool {
		return logs.FilterMessage("could not connect to discovered peer").Len() > 1
	}, 1*time.Second, 100*time.Millisecond)
}
