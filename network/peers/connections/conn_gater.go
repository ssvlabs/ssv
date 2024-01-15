package connections

import (
	"time"

	"runtime"

	pubsub "github.com/libp2p/go-libp2p-pubsub"
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
type ConnectionGater struct {
	logger    *zap.Logger
	atLimit   func() bool
	isBad     func(id peer.ID) bool
	ipLimiter *leakybucket.Collector
	blackList pubsub.Blacklist
}

// NewConnectionGater creates a new instance of ConnectionGater
func NewConnectionGater(logger *zap.Logger, atLimit func() bool, isBad func(id peer.ID) bool) *ConnectionGater {
	blackList, _ := pubsub.NewTimeCachedBlacklist(60 * time.Minute)
	return &ConnectionGater{
		logger:    logger,
		atLimit:   atLimit,
		isBad:     isBad,
		ipLimiter: leakybucket.NewCollector(ipLimitRate, ipLimitBurst, ipLimitPeriod, true),
		blackList: blackList,
	}
}

func (n *ConnectionGater) BlockPeer(id peer.ID) {
	if !n.blackList.Add(id) {
		n.logger.Debug("can not blacklist peer")
	}
}

func (n *ConnectionGater) IsPeerBlocked(id peer.ID) bool {
	return n.blackList.Contains(id)
}

// InterceptPeerDial is called on an imminent outbound peer dial request, prior
// to the addresses of that peer being available/resolved. Blocking connections
// at this stage is typical for blacklisting scenarios
func (n *ConnectionGater) InterceptPeerDial(id peer.ID) bool {
	if n.IsPeerBlocked(id) {
		return false
	}
	return !n.atLimit()
}

// InterceptAddrDial is called on an imminent outbound dial to a peer on a
// particular address. Blocking connections at this stage is typical for
// address filtering.
func (n *ConnectionGater) InterceptAddrDial(id peer.ID, multiaddr ma.Multiaddr) bool {
	if n.IsPeerBlocked(id) {
		return false
	}
	return !n.atLimit()
}

// InterceptAccept is called as soon as a transport listener receives an
// inbound connection request, before any upgrade takes place. Transports who
// accept already secure and/or multiplexed connections (e.g. possibly QUIC)
// MUST call this method regardless, for correctness/consistency.
func (n *ConnectionGater) InterceptAccept(multiaddrs libp2pnetwork.ConnMultiaddrs) bool {
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
func (n *ConnectionGater) InterceptSecured(direction libp2pnetwork.Direction, id peer.ID, multiaddrs libp2pnetwork.ConnMultiaddrs) bool {
	if n.IsPeerBlocked(id) {
		return false
	}
	return !n.isBad(id)
}

// InterceptUpgraded is called for inbound and outbound connections, after
// libp2p has finished upgrading the connection entirely to a secure,
// multiplexed channel.
func (n *ConnectionGater) InterceptUpgraded(conn libp2pnetwork.Conn) (bool, control.DisconnectReason) {
	return true, 0
}

func (n *ConnectionGater) validateDial(addr multiaddr.Multiaddr) bool {
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
