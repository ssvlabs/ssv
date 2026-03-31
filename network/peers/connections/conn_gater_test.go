package connections

import (
	"testing"
	"time"

	libp2pnetwork "github.com/libp2p/go-libp2p/core/network"
	"github.com/libp2p/go-libp2p/core/peer"
	leakybucket "github.com/prysmaticlabs/prysm/v4/container/leaky-bucket"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	"github.com/ssvlabs/ssv/utils/ttl"
)

func TestConnGaterInterceptAddrDial(t *testing.T) {
	gater := &connGater{
		logger:          zap.NewNop(),
		isBadPeer:       func(id peer.ID) bool { return id == "bad-peer" },
		trimmedRecently: ttl.New[peer.ID, struct{}](t.Context(), time.Minute, time.Minute),
	}

	require.False(t, gater.InterceptAddrDial("bad-peer", mustMultiaddr("/ip4/127.0.0.1/tcp/13000")))
	require.True(t, gater.InterceptAddrDial("good-peer", mustMultiaddr("/ip4/127.0.0.1/tcp/13000")))
}

func TestConnGaterInterceptPeerDial(t *testing.T) {
	gater := &connGater{}

	require.True(t, gater.InterceptPeerDial("peer"))
}

func TestConnGaterInterceptAcceptHonorsLimits(t *testing.T) {
	t.Run("disabled bypasses limits", func(t *testing.T) {
		gater := &connGater{
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
			logger:          zap.NewNop(),
			atMaxPeersLimit: func() bool { return false },
			atInboundLimit:  func() bool { return true },
			trimmedRecently: ttl.New[peer.ID, struct{}](t.Context(), time.Minute, time.Minute),
		}

		require.False(t, gater.InterceptAccept(&testConn{remoteMultiaddr: mustMultiaddr("/ip4/127.0.0.1/tcp/13000")}))
	})

	t.Run("rejects at max peers limit after a valid dial", func(t *testing.T) {
		gater := &connGater{
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

func TestConnGaterInterceptAcceptRejectsDNSAddresses(t *testing.T) {
	gater := &connGater{
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
	trimmedRecently.Set("trimmed-peer", struct{}{})

	gater := &connGater{
		logger:          zap.NewNop(),
		isBadPeer:       func(id peer.ID) bool { return id == "bad-peer" },
		trimmedRecently: trimmedRecently,
	}

	require.False(t, gater.InterceptSecured(libp2pnetwork.DirInbound, "trimmed-peer", nil))
	require.False(t, gater.InterceptSecured(libp2pnetwork.DirInbound, "bad-peer", nil))
	require.True(t, gater.InterceptSecured(libp2pnetwork.DirOutbound, "good-peer", nil))
}

func TestConnGaterInterceptUpgraded(t *testing.T) {
	gater := &connGater{}

	allowed, reason := gater.InterceptUpgraded(&testConn{})
	require.True(t, allowed)
	require.Zero(t, reason)
}
