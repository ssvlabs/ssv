package connections

import (
	"runtime"
	"time"

	"github.com/libp2p/go-libp2p/core/connmgr"
	"github.com/libp2p/go-libp2p/core/control"
	libp2pnetwork "github.com/libp2p/go-libp2p/core/network"
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/multiformats/go-multiaddr"
	ma "github.com/multiformats/go-multiaddr"
	manet "github.com/multiformats/go-multiaddr/net"
	leakybucket "github.com/prysmaticlabs/prysm/v4/container/leaky-bucket"
	"go.uber.org/zap"
)

const (
	// Rate, burst and period over which we allow inbound connections from a single IP.
	ipLimitRate   = 4
	ipLimitBurst  = 8
	ipLimitPeriod = 30 * time.Second

	//
)

// connGater implements ConnectionGater interface:
// https://github.com/libp2p/go-libp2p/core/blob/master/connmgr/gater.go
type connGater struct {
	logger    *zap.Logger // struct logger to implement connmgr.ConnectionGater
	atLimit   func() bool
	ipLimiter *leakybucket.Collector
}

// NewConnectionGater creates a new instance of ConnectionGater
func NewConnectionGater(logger *zap.Logger, atLimit func() bool) connmgr.ConnectionGater {
	return &connGater{
		logger:    logger,
		atLimit:   atLimit,
		ipLimiter: leakybucket.NewCollector(ipLimitRate, ipLimitBurst, ipLimitPeriod, true),
	}
}

// InterceptPeerDial is called on an imminent outbound peer dial request, prior
// to the addresses of that peer being available/resolved. Blocking connections
// at this stage is typical for blacklisting scenarios
func (n *connGater) InterceptPeerDial(id peer.ID) bool {
	return true
}

// InterceptAddrDial is called on an imminent outbound dial to a peer on a
// particular address. Blocking connections at this stage is typical for
// address filtering.
func (n *connGater) InterceptAddrDial(id peer.ID, multiaddr ma.Multiaddr) bool {
	return true
}

// InterceptAccept is called as soon as a transport listener receives an
// inbound connection request, before any upgrade takes place. Transports who
// accept already secure and/or multiplexed connections (e.g. possibly QUIC)
// MUST call this method regardless, for correctness/consistency.
func (n *connGater) InterceptAccept(multiaddrs libp2pnetwork.ConnMultiaddrs) bool {
	remoteAddr := multiaddrs.RemoteMultiaddr()
	if !n.validateDial(remoteAddr) {
		// Yield this goroutine to allow others to run in-between connection attempts.
		runtime.Gosched()

		n.logger.Debug("connection rejected due to IP rate limit", zap.String("remote_addr", remoteAddr.String()))
		return false
	}
	return !n.atLimit()
}

// InterceptSecured is called for both inbound and outbound connections,
// after a security handshake has taken place and we've authenticated the peer.
func (n *connGater) InterceptSecured(direction libp2pnetwork.Direction, id peer.ID, multiaddrs libp2pnetwork.ConnMultiaddrs) bool {
	return true
}

// InterceptUpgraded is called for inbound and outbound connections, after
// libp2p has finished upgrading the connection entirely to a secure,
// multiplexed channel.
func (n *connGater) InterceptUpgraded(conn libp2pnetwork.Conn) (bool, control.DisconnectReason) {
	return true, 0
}

func (n *connGater) validateDial(addr multiaddr.Multiaddr) bool {
	ip, err := manet.ToIP(addr)
	if err != nil {
		return false
	}
	remaining := n.ipLimiter.Remaining(ip.String())
	if remaining <= 0 {
		return false
	}
	n.ipLimiter.Add(ip.String(), 1)
	return true
}
