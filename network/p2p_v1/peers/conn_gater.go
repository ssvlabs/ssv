package peers

import (
	"github.com/kevinms/leakybucket-go"
	"github.com/libp2p/go-libp2p-core/connmgr"
	"github.com/libp2p/go-libp2p-core/control"
	libp2pnetwork "github.com/libp2p/go-libp2p-core/network"
	"github.com/libp2p/go-libp2p-core/peer"
	ma "github.com/multiformats/go-multiaddr"
	manet "github.com/multiformats/go-multiaddr/net"
	"go.uber.org/zap"
)

const (
	// Limit for rate limiter when processing new inbound dials.
	ipLimit = 4
	// Burst limit for inbound dials.
	ipBurst = 8
)

// ConnGater implements ConnectionGater interface:
// https://github.com/libp2p/go-libp2p-core/blob/master/connmgr/gater.go
type ConnGater interface {
	connmgr.ConnectionGater
	// WithConnIndex enables to inject a connection index
	WithConnIndex(idx ConnectionIndex)
}

// connGater implements ConnGater
type connGater struct {
	logger *zap.Logger
	idx    ConnectionIndex

	ipLimiter *leakybucket.Collector
}

// NewConnectionGater creates a new instance of ConnectionGater
func NewConnectionGater(logger *zap.Logger, ipRateLimit bool) ConnGater {
	cg := &connGater{
		logger: logger,
		idx:    new(optimisticConnIndex),
	}
	if ipRateLimit {
		cg.ipLimiter = leakybucket.NewCollector(ipLimit, ipBurst, true)
	}
	return cg
}

// WithConnIndex enables to inject a connection index
func (cg *connGater) WithConnIndex(idx ConnectionIndex) {
	cg.idx = idx
}

// InterceptPeerDial is called on an imminent outbound peer dial request, prior
// to the addresses of that peer being available/resolved. Blocking connections
// at this stage is typical for blacklisting scenarios
func (cg *connGater) InterceptPeerDial(id peer.ID) bool {
	return !cg.idx.IsBad(id)
}

// InterceptAddrDial is called on an imminent outbound dial to a peer on a
// particular address. Blocking connections at this stage is typical for
// address filtering.
func (cg *connGater) InterceptAddrDial(id peer.ID, multiaddr ma.Multiaddr) bool {
	return true
}

// InterceptAccept is called as soon as a transport listener receives an
// inbound connection request, before any upgrade takes place.
//
// In our case, an ip rate limiting will be applied on every inbound connection.
func (cg *connGater) InterceptAccept(multiaddrs libp2pnetwork.ConnMultiaddrs) bool {
	addr, ipFilter := cg.filterIPAddr(multiaddrs.RemoteMultiaddr())
	if !ipFilter {
		cg.logger.Debug("IP exceeded limits", zap.String("addr", addr))
		return false
	}
	return true
}

// InterceptSecured is called for both inbound and outbound connections,
// after a security handshake has taken place and we've authenticated the peer.
func (cg *connGater) InterceptSecured(direction libp2pnetwork.Direction, id peer.ID, multiaddrs libp2pnetwork.ConnMultiaddrs) bool {
	return !cg.idx.IsBad(id)
}

// InterceptUpgraded is called for inbound and outbound connections, after
// libp2p has finished upgrading the connection entirely to a secure,
// multiplexed channel.
func (cg *connGater) InterceptUpgraded(conn libp2pnetwork.Conn) (bool, control.DisconnectReason) {
	return true, 0
}

func (cg *connGater) filterIPAddr(addr ma.Multiaddr) (string, bool) {
	ip, err := manet.ToIP(addr)
	if err != nil {
		return "", false
	}
	ipStr := ip.String()
	if cg.ipLimiter == nil {
		return ipStr, true
	}
	remaining := cg.ipLimiter.Remaining(ipStr)
	if remaining <= 0 {
		return ipStr, false
	}
	cg.ipLimiter.Add(ip.String(), 1)
	return ipStr, true
}

// optimisticConnIndex returns positive results (i.e. accept all connections)
type optimisticConnIndex struct {
}

func (oci *optimisticConnIndex) CanConnect(id peer.ID) bool {
	return true
}

func (oci *optimisticConnIndex) Limit(dir libp2pnetwork.Direction) bool {
	return false
}

func (oci *optimisticConnIndex) IsBad(id peer.ID) bool {
	return false
}
