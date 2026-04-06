package connections

import (
	"testing"
	"time"

	libp2pnetwork "github.com/libp2p/go-libp2p/core/network"
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	"github.com/ssvlabs/ssv/network/commons"
	"github.com/ssvlabs/ssv/network/discovery"
	"github.com/ssvlabs/ssv/network/peers"
	"github.com/ssvlabs/ssv/network/peers/connections/mock"
	"github.com/ssvlabs/ssv/utils/ttl"
)

func TestConnHandlerHandleOutboundConnection(t *testing.T) {
	pid := peer.ID("peer-outbound")
	peerInfos := peers.NewPeerInfoIndex()
	discoveredPeers := ttl.New[peer.ID, discovery.DiscoveredPeer](t.Context(), time.Minute, time.Minute)
	discoveredPeers.Set(pid, discovery.DiscoveredPeer{})
	connIdx := &mock.MockConnectionIndex{}

	handshaker := &testHandshaker{}
	handler := NewConnHandler(
		t.Context(),
		zap.NewNop(),
		handshaker,
		func() commons.Subnets { return commons.ZeroSubnets },
		peers.NewSubnetsIndex(),
		connIdx,
		peerInfos,
		discoveredPeers,
	)

	net := newTestNetwork()
	conn := &testConn{
		remotePeer:      pid,
		remoteMultiaddr: mustMultiaddr("/ip4/127.0.0.1/tcp/13000"),
		stats:           libp2pnetwork.ConnStats{Stats: libp2pnetwork.Stats{Direction: libp2pnetwork.DirOutbound}},
	}

	handler.Handle().ConnectedF(net, conn)

	require.Eventually(t, func() bool {
		return peerInfos.State(pid) == peers.StateConnected
	}, 3*time.Second, 10*time.Millisecond)
	require.Equal(t, 1, handshaker.CallCount())
	require.Eventually(t, func() bool {
		return !discoveredPeers.Has(pid)
	}, 3*time.Second, 10*time.Millisecond)
}

func TestConnHandlerHandleOutboundConnectionHandshakeFailureDisconnects(t *testing.T) {
	pid := peer.ID("peer-outbound-fail")
	peerInfos := peers.NewPeerInfoIndex()
	handshaker := &testHandshaker{err: errTestHandshake}
	handler := NewConnHandler(
		t.Context(),
		zap.NewNop(),
		handshaker,
		func() commons.Subnets { return commons.ZeroSubnets },
		peers.NewSubnetsIndex(),
		&mock.MockConnectionIndex{},
		peerInfos,
		ttl.New[peer.ID, discovery.DiscoveredPeer](t.Context(), time.Minute, time.Minute),
	)

	net := newTestNetwork()
	conn := &testConn{
		remotePeer:      pid,
		remoteMultiaddr: mustMultiaddr("/ip4/127.0.0.1/tcp/13010"),
		stats:           libp2pnetwork.ConnStats{Stats: libp2pnetwork.Stats{Direction: libp2pnetwork.DirOutbound}},
	}

	handler.Handle().ConnectedF(net, conn)

	require.Eventually(t, func() bool {
		return len(net.ClosedPeers()) == 1
	}, 3*time.Second, 10*time.Millisecond)
	require.Equal(t, []peer.ID{pid}, net.ClosedPeers())
	require.Never(t, func() bool {
		return peerInfos.State(pid) == peers.StateConnected
	}, 250*time.Millisecond, 10*time.Millisecond)
}

func TestConnHandlerHandleOutboundConnectionRejectsAtLimit(t *testing.T) {
	pid := peer.ID("peer-outbound-at-limit")
	peerInfos := peers.NewPeerInfoIndex()
	handshaker := &testHandshaker{}
	handler := NewConnHandler(
		t.Context(),
		zap.NewNop(),
		handshaker,
		func() commons.Subnets { return commons.ZeroSubnets },
		peers.NewSubnetsIndex(),
		&mock.MockConnectionIndex{LimitValue: true},
		peerInfos,
		ttl.New[peer.ID, discovery.DiscoveredPeer](t.Context(), time.Minute, time.Minute),
	)

	net := newTestNetwork()
	conn := &testConn{
		remotePeer:      pid,
		remoteMultiaddr: mustMultiaddr("/ip4/127.0.0.1/tcp/13011"),
		stats:           libp2pnetwork.ConnStats{Stats: libp2pnetwork.Stats{Direction: libp2pnetwork.DirOutbound}},
	}

	handler.Handle().ConnectedF(net, conn)

	require.Eventually(t, func() bool {
		return len(net.ClosedPeers()) == 1
	}, 3*time.Second, 10*time.Millisecond)
	require.Equal(t, []peer.ID{pid}, net.ClosedPeers())
	require.Never(t, func() bool {
		return peerInfos.State(pid) == peers.StateConnected
	}, 250*time.Millisecond, 10*time.Millisecond)
}

func TestConnHandlerHandleDeduplicatesConcurrentOutboundHandshakes(t *testing.T) {
	pid := peer.ID("peer-dedup")
	peerInfos := peers.NewPeerInfoIndex()
	handshaker := &testHandshaker{
		started: make(chan struct{}, 2),
		release: make(chan struct{}),
	}
	handler := NewConnHandler(
		t.Context(),
		zap.NewNop(),
		handshaker,
		func() commons.Subnets { return commons.ZeroSubnets },
		peers.NewSubnetsIndex(),
		&mock.MockConnectionIndex{},
		peerInfos,
		ttl.New[peer.ID, discovery.DiscoveredPeer](t.Context(), time.Minute, time.Minute),
	)

	net := newTestNetwork()
	conn := &testConn{
		remotePeer:      pid,
		remoteMultiaddr: mustMultiaddr("/ip4/127.0.0.1/tcp/13000"),
		stats:           libp2pnetwork.ConnStats{Stats: libp2pnetwork.Stats{Direction: libp2pnetwork.DirOutbound}},
	}

	notify := handler.Handle()
	notify.ConnectedF(net, conn)
	<-handshaker.started
	notify.ConnectedF(net, conn)

	// ConnectedF handles connections on background goroutines. Once the first
	// handshake has started and is blocked, the second attempt should be ignored
	// and must not start another handshake during this scheduling window.
	require.Never(t, func() bool {
		return handshaker.CallCount() > 1
	}, 250*time.Millisecond, 10*time.Millisecond)

	close(handshaker.release)

	require.Eventually(t, func() bool {
		return peerInfos.State(pid) == peers.StateConnected
	}, 3*time.Second, 10*time.Millisecond)
}

func TestConnHandlerHandleInboundConnectionWaitsForHandshake(t *testing.T) {
	pid := peer.ID("peer-inbound")
	peerInfos := peers.NewPeerInfoIndex()
	subnetsIndex := peers.NewSubnetsIndex()
	handshaker := &testHandshaker{}
	mySubnets := commons.ZeroSubnets
	mySubnets.Set(1)
	peerSubnets := commons.ZeroSubnets
	peerSubnets.Set(1)
	subnetsIndex.UpdatePeerSubnets(pid, peerSubnets)

	handler := NewConnHandler(
		t.Context(),
		zap.NewNop(),
		handshaker,
		func() commons.Subnets { return mySubnets },
		subnetsIndex,
		&mock.MockConnectionIndex{},
		peerInfos,
		ttl.New[peer.ID, discovery.DiscoveredPeer](t.Context(), time.Minute, time.Minute),
	)

	net := newTestNetwork()
	net.connectedness[pid] = libp2pnetwork.Connected
	conn := &testConn{
		remotePeer:      pid,
		remoteMultiaddr: mustMultiaddr("/ip4/127.0.0.1/tcp/13001"),
		stats:           libp2pnetwork.ConnStats{Stats: libp2pnetwork.Stats{Direction: libp2pnetwork.DirInbound}},
	}

	bundle := handler.Handle()
	bundle.ConnectedF(net, conn)

	require.Eventually(t, func() bool {
		return peerInfos.State(pid) == peers.StateDisconnected
	}, 3*time.Second, 10*time.Millisecond)
	require.NotEqual(t, peers.StateConnected, peerInfos.State(pid))
	require.Equal(t, 0, handshaker.CallCount(), "inbound path must not initiate handshake")

	peerInfos.UpdatePeerInfo(pid, func(info *peers.PeerInfo) {
		info.LastHandshake = time.Now().Add(time.Hour)
		info.LastHandshakeError = nil
	})

	require.Eventually(t, func() bool {
		return peerInfos.State(pid) == peers.StateConnected
	}, 3*time.Second, 10*time.Millisecond)
	require.Equal(t, 0, handshaker.CallCount(), "inbound path must not initiate handshake")
	require.Empty(t, net.ClosedPeers())
}

func TestConnHandlerHandleInboundConnectionRejectsPeerWithoutSharedSubnets(t *testing.T) {
	pid := peer.ID("peer-inbound-no-shared")
	peerInfos := peers.NewPeerInfoIndex()
	subnetsIndex := peers.NewSubnetsIndex()
	handshaker := &testHandshaker{}
	mySubnets := commons.ZeroSubnets
	mySubnets.Set(1)
	peerSubnets := commons.ZeroSubnets
	peerSubnets.Set(2)
	subnetsIndex.UpdatePeerSubnets(pid, peerSubnets)

	handler := NewConnHandler(
		t.Context(),
		zap.NewNop(),
		handshaker,
		func() commons.Subnets { return mySubnets },
		subnetsIndex,
		&mock.MockConnectionIndex{},
		peerInfos,
		ttl.New[peer.ID, discovery.DiscoveredPeer](t.Context(), time.Minute, time.Minute),
	)

	net := newTestNetwork()
	net.connectedness[pid] = libp2pnetwork.Connected
	conn := &testConn{
		remotePeer:      pid,
		remoteMultiaddr: mustMultiaddr("/ip4/127.0.0.1/tcp/13002"),
		stats:           libp2pnetwork.ConnStats{Stats: libp2pnetwork.Stats{Direction: libp2pnetwork.DirInbound}},
	}

	bundle := handler.Handle()
	bundle.ConnectedF(net, conn)

	require.Eventually(t, func() bool {
		return peerInfos.State(pid) == peers.StateDisconnected
	}, 3*time.Second, 10*time.Millisecond)
	require.NotEqual(t, peers.StateConnected, peerInfos.State(pid))
	require.Equal(t, 0, handshaker.CallCount(), "inbound path must not initiate handshake")

	peerInfos.UpdatePeerInfo(pid, func(info *peers.PeerInfo) {
		info.LastHandshake = time.Now().Add(time.Hour)
		info.LastHandshakeError = nil
	})

	require.Eventually(t, func() bool {
		return len(net.ClosedPeers()) == 1
	}, 3*time.Second, 10*time.Millisecond)
	require.Equal(t, []peer.ID{pid}, net.ClosedPeers())
	require.Equal(t, 0, handshaker.CallCount(), "inbound path must not initiate handshake")
	require.Equal(t, peers.StateDisconnected, peerInfos.State(pid))
}

func TestConnHandlerDisconnectedF(t *testing.T) {
	pid := peer.ID("peer-disconnected")
	peerInfos := peers.NewPeerInfoIndex()
	peerInfos.SetState(pid, peers.StateConnected)

	handler := NewConnHandler(
		t.Context(),
		zap.NewNop(),
		&testHandshaker{},
		func() commons.Subnets { return commons.ZeroSubnets },
		peers.NewSubnetsIndex(),
		&mock.MockConnectionIndex{},
		peerInfos,
		ttl.New[peer.ID, discovery.DiscoveredPeer](t.Context(), time.Minute, time.Minute),
	)
	bundle := handler.Handle()

	net := newTestNetwork()
	conn := &testConn{
		remotePeer:      pid,
		remoteMultiaddr: mustMultiaddr("/ip4/127.0.0.1/tcp/13003"),
		stats:           libp2pnetwork.ConnStats{Stats: libp2pnetwork.Stats{Direction: libp2pnetwork.DirInbound}},
	}

	net.connectedness[pid] = libp2pnetwork.Connected
	bundle.DisconnectedF(net, conn)
	require.Equal(t, peers.StateConnected, peerInfos.State(pid))

	net.connectedness[pid] = libp2pnetwork.NotConnected
	bundle.DisconnectedF(net, conn)
	require.Equal(t, peers.StateDisconnected, peerInfos.State(pid))
}

func TestConnHandlerHandleIgnoresAlreadyConnectedPeer(t *testing.T) {
	pid := peer.ID("peer-already-connected")
	peerInfos := peers.NewPeerInfoIndex()
	peerInfos.SetState(pid, peers.StateConnected)
	handshaker := &testHandshaker{}
	handler := NewConnHandler(
		t.Context(),
		zap.NewNop(),
		handshaker,
		func() commons.Subnets { return commons.ZeroSubnets },
		peers.NewSubnetsIndex(),
		&mock.MockConnectionIndex{},
		peerInfos,
		ttl.New[peer.ID, discovery.DiscoveredPeer](t.Context(), time.Minute, time.Minute),
	)

	net := newTestNetwork()
	conn := &testConn{
		remotePeer:      pid,
		remoteMultiaddr: mustMultiaddr("/ip4/127.0.0.1/tcp/13012"),
		stats:           libp2pnetwork.ConnStats{Stats: libp2pnetwork.Stats{Direction: libp2pnetwork.DirOutbound}},
	}

	handler.Handle().ConnectedF(net, conn)

	require.Never(t, func() bool {
		return handshaker.CallCount() > 0
	}, 250*time.Millisecond, 10*time.Millisecond)
	require.Empty(t, net.ClosedPeers())
	require.Equal(t, peers.StateConnected, peerInfos.State(pid))
}

func TestConnHandlerHandleIgnoresAlreadyConnectingPeer(t *testing.T) {
	pid := peer.ID("peer-already-connecting")
	peerInfos := peers.NewPeerInfoIndex()
	peerInfos.SetState(pid, peers.StateConnecting)
	handshaker := &testHandshaker{}
	handler := NewConnHandler(
		t.Context(),
		zap.NewNop(),
		handshaker,
		func() commons.Subnets { return commons.ZeroSubnets },
		peers.NewSubnetsIndex(),
		&mock.MockConnectionIndex{},
		peerInfos,
		ttl.New[peer.ID, discovery.DiscoveredPeer](t.Context(), time.Minute, time.Minute),
	)

	net := newTestNetwork()
	conn := &testConn{
		remotePeer:      pid,
		remoteMultiaddr: mustMultiaddr("/ip4/127.0.0.1/tcp/13013"),
		stats:           libp2pnetwork.ConnStats{Stats: libp2pnetwork.Stats{Direction: libp2pnetwork.DirOutbound}},
	}

	handler.Handle().ConnectedF(net, conn)

	require.Never(t, func() bool {
		return handshaker.CallCount() > 0
	}, 250*time.Millisecond, 10*time.Millisecond)
	require.Empty(t, net.ClosedPeers())
	require.Equal(t, peers.StateConnecting, peerInfos.State(pid))
}

func TestConnHandlerSharesEnoughSubnets(t *testing.T) {
	pid := peer.ID("peer-subnets")
	subnetsIndex := peers.NewSubnetsIndex()
	handler := &connHandler{
		logger:          zap.NewNop(),
		subnetsProvider: func() commons.Subnets { return commons.ZeroSubnets },
		subnetsIndex:    subnetsIndex,
	}
	conn := &testConn{remotePeer: pid}

	require.False(t, handler.sharesEnoughSubnets(conn))

	peerSubnets := commons.ZeroSubnets
	peerSubnets.Set(2)
	subnetsIndex.UpdatePeerSubnets(pid, peerSubnets)
	require.True(t, handler.sharesEnoughSubnets(conn))

	mySubnets := commons.ZeroSubnets
	mySubnets.Set(3)
	handler.subnetsProvider = func() commons.Subnets { return mySubnets }
	require.False(t, handler.sharesEnoughSubnets(conn))

	mySubnets.Set(2)
	handler.subnetsProvider = func() commons.Subnets { return mySubnets }
	require.True(t, handler.sharesEnoughSubnets(conn))
}
