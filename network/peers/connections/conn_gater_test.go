package connections

import (
	"testing"
	"time"

	"github.com/libp2p/go-libp2p/core/crypto"
	libp2pnetwork "github.com/libp2p/go-libp2p/core/network"
	"github.com/libp2p/go-libp2p/core/peer"
	leakybucket "github.com/prysmaticlabs/prysm/v4/container/leaky-bucket"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	zapobserver "go.uber.org/zap/zaptest/observer"

	"github.com/ssvlabs/ssv/network/peers/peertrace"
	"github.com/ssvlabs/ssv/utils/ttl"
)

const (
	badPeerID     peer.ID = "bad-peer"
	goodPeerID    peer.ID = "good-peer"
	trimmedPeerID peer.ID = "trimmed-peer"
)

func TestConnGaterInterceptAddrDial(t *testing.T) {
	gater := &connGater{
		ctx:             t.Context(),
		logger:          zap.NewNop(),
		isBadPeer:       func(id peer.ID) bool { return id == badPeerID },
		trimmedRecently: ttl.New[peer.ID, struct{}](t.Context(), time.Minute, time.Minute),
	}

	require.False(t, gater.InterceptAddrDial(badPeerID, mustMultiaddr("/ip4/127.0.0.1/tcp/13000")))
	require.True(t, gater.InterceptAddrDial(goodPeerID, mustMultiaddr("/ip4/127.0.0.1/tcp/13000")))
}

func TestConnGaterInterceptPeerDial(t *testing.T) {
	gater := &connGater{
		ctx:             t.Context(),
		logger:          zap.NewNop(),
		isBadPeer:       func(id peer.ID) bool { return id == badPeerID },
		trimmedRecently: ttl.New[peer.ID, struct{}](t.Context(), time.Minute, time.Minute),
	}

	require.False(t, gater.InterceptPeerDial(badPeerID))
	require.True(t, gater.InterceptPeerDial(goodPeerID))
}

func TestConnGaterInterceptAcceptHonorsLimits(t *testing.T) {
	t.Run("disabled bypasses limits", func(t *testing.T) {
		gater := &connGater{
			ctx:             t.Context(),
			logger:          zap.NewNop(),
			disable:         true,
			atMaxPeersLimit: func() bool { return true },
			atInboundLimit:  func() bool { return true },
			trimmedRecently: ttl.New[peer.ID, struct{}](t.Context(), time.Minute, time.Minute),
		}

		require.True(t, gater.InterceptAccept(&testConn{remoteMultiaddr: mustMultiaddr("/ip4/127.0.0.1/tcp/13000")}))
	})

	t.Run("rejects at inbound limit", func(t *testing.T) {
		gater := &connGater{
			ctx:             t.Context(),
			logger:          zap.NewNop(),
			atMaxPeersLimit: func() bool { return false },
			atInboundLimit:  func() bool { return true },
			trimmedRecently: ttl.New[peer.ID, struct{}](t.Context(), time.Minute, time.Minute),
		}

		require.False(t, gater.InterceptAccept(&testConn{remoteMultiaddr: mustMultiaddr("/ip4/127.0.0.1/tcp/13000")}))
	})

	t.Run("rejects at max peers limit after a valid dial", func(t *testing.T) {
		gater := &connGater{
			ctx:             t.Context(),
			logger:          zap.NewNop(),
			atMaxPeersLimit: func() bool { return true },
			atInboundLimit:  func() bool { return false },
			ipLimiter:       leakybucket.NewCollector(ipLimitRate, ipLimitBurst, ipLimitPeriod, true),
			trimmedRecently: ttl.New[peer.ID, struct{}](t.Context(), time.Minute, time.Minute),
		}

		require.False(t, gater.InterceptAccept(&testConn{remoteMultiaddr: mustMultiaddr("/ip4/127.0.0.1/tcp/13000")}))
	})
}

func TestConnGaterInterceptAcceptRateLimitsByIP(t *testing.T) {
	gater := &connGater{
		ctx:             t.Context(),
		logger:          zap.NewNop(),
		atMaxPeersLimit: func() bool { return false },
		atInboundLimit:  func() bool { return false },
		ipLimiter:       leakybucket.NewCollector(ipLimitRate, ipLimitBurst, ipLimitPeriod, true),
		trimmedRecently: ttl.New[peer.ID, struct{}](t.Context(), time.Minute, time.Minute),
	}

	sameIPConn := &testConn{remoteMultiaddr: mustMultiaddr("/ip4/192.0.2.1/tcp/13000")}
	for range ipLimitBurst {
		require.True(t, gater.InterceptAccept(sameIPConn))
	}
	require.False(t, gater.InterceptAccept(sameIPConn))

	otherIPConn := &testConn{remoteMultiaddr: mustMultiaddr("/ip4/192.0.2.2/tcp/13000")}
	require.True(t, gater.InterceptAccept(otherIPConn))
}

func TestConnGaterValidateDialReturnsReason(t *testing.T) {
	gater := &connGater{
		ipLimiter: leakybucket.NewCollector(ipLimitRate, ipLimitBurst, ipLimitPeriod, true),
	}

	allowed, reason := gater.validateDial(mustMultiaddr("/dns4/example.com/tcp/13000"))
	require.False(t, allowed)
	require.Equal(t, connectionGaterReasonInvalidRemoteAddr, reason)

	addr := mustMultiaddr("/ip4/192.0.2.1/tcp/13000")
	for range ipLimitBurst {
		allowed, reason = gater.validateDial(addr)
		require.True(t, allowed)
		require.Equal(t, connectionGaterReasonAllowed, reason)
	}
	allowed, reason = gater.validateDial(addr)
	require.False(t, allowed)
	require.Equal(t, connectionGaterReasonIPRateLimit, reason)
}

func TestConnGaterInterceptAcceptRejectsDNSAddresses(t *testing.T) {
	gater := &connGater{
		ctx:             t.Context(),
		logger:          zap.NewNop(),
		atMaxPeersLimit: func() bool { return false },
		atInboundLimit:  func() bool { return false },
		ipLimiter:       leakybucket.NewCollector(ipLimitRate, ipLimitBurst, ipLimitPeriod, true),
		trimmedRecently: ttl.New[peer.ID, struct{}](t.Context(), time.Minute, time.Minute),
	}

	require.False(t, gater.InterceptAccept(&testConn{remoteMultiaddr: mustMultiaddr("/dns4/example.com/tcp/13000")}))
}

func TestConnGaterInterceptSecured(t *testing.T) {
	trimmedRecently := ttl.New[peer.ID, struct{}](t.Context(), time.Minute, time.Minute)
	trimmedRecently.Set(trimmedPeerID, struct{}{})

	gater := &connGater{
		ctx:             t.Context(),
		logger:          zap.NewNop(),
		isBadPeer:       func(id peer.ID) bool { return id == badPeerID },
		trimmedRecently: trimmedRecently,
	}

	require.False(t, gater.InterceptSecured(libp2pnetwork.DirInbound, trimmedPeerID, nil))
	require.False(t, gater.InterceptSecured(libp2pnetwork.DirInbound, badPeerID, nil))
	require.True(t, gater.InterceptSecured(libp2pnetwork.DirOutbound, goodPeerID, nil))
}

func TestConnGaterInterceptSecuredLogsHighlightedPeerDecision(t *testing.T) {
	_, publicKey, err := crypto.GenerateSecp256k1Key(nil)
	require.NoError(t, err)
	highlightedPeerID, err := peer.IDFromPublicKey(publicKey)
	require.NoError(t, err)

	peerObserver, err := peertrace.New(peertrace.Config{Peers: highlightedPeerID.String()})
	require.NoError(t, err)

	core, logs := zapobserver.New(zap.InfoLevel)
	gater := &connGater{
		ctx:             t.Context(),
		logger:          zap.New(core),
		isBadPeer:       func(id peer.ID) bool { return id == highlightedPeerID },
		trimmedRecently: ttl.New[peer.ID, struct{}](t.Context(), time.Minute, time.Minute),
		peerObserver:    peerObserver,
	}

	require.False(t, gater.InterceptSecured(libp2pnetwork.DirInbound, highlightedPeerID, nil))

	require.Len(t, logs.All(), 1)
	require.Equal(t, "p2p highlighted peer event", logs.All()[0].Message)
	fields := logs.All()[0].ContextMap()
	require.Equal(t, true, fields["p2p_highlight"])
	require.Equal(t, "connection_gater_decision", fields["p2p_highlight_event"])
	require.Equal(t, highlightedPeerID.String(), fields["peer_id"])
	require.Equal(t, connectionGaterPhaseSecured, fields["connection_gater_phase"])
	require.Equal(t, connectionGaterDecisionReject, fields["connection_gater_decision"])
	require.Equal(t, connectionGaterReasonBadPeer, fields["connection_gater_reason"])
	require.Equal(t, libp2pnetwork.DirInbound.String(), fields["conn_direction"])
}

func TestConnGaterInterceptUpgraded(t *testing.T) {
	gater := &connGater{}

	allowed, reason := gater.InterceptUpgraded(&testConn{})
	require.True(t, allowed)
	require.Zero(t, reason)
}
