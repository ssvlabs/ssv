package connections

import (
	"github.com/bloxapp/ssv/network/peers"
	"github.com/libp2p/go-libp2p/core/connmgr"
	"github.com/libp2p/go-libp2p/core/control"
	libp2pnetwork "github.com/libp2p/go-libp2p/core/network"
	"github.com/libp2p/go-libp2p/core/peer"
	ma "github.com/multiformats/go-multiaddr"
	"go.uber.org/zap"
)

// connGater implements ConnectionGater interface:
// https://github.com/libp2p/go-libp2p/core/blob/master/connmgr/gater.go
// TODO: add IP limiting
type connGater struct {
	logger *zap.Logger // struct logger to implement connmgr.ConnectionGater
	idx    peers.ConnectionIndex
}

// NewConnectionGater creates a new instance of ConnectionGater
func NewConnectionGater(logger *zap.Logger, idx peers.ConnectionIndex) connmgr.ConnectionGater {
	return &connGater{
		logger: logger,
		idx:    idx,
	}
}

// InterceptPeerDial is called on an imminent outbound peer dial request, prior
// to the addresses of that peer being available/resolved. Blocking connections
// at this stage is typical for blacklisting scenarios
func (n *connGater) InterceptPeerDial(id peer.ID) bool {
	return n.idx.Limit(libp2pnetwork.DirOutbound)
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
	return n.idx.Limit(libp2pnetwork.DirInbound)
}

// InterceptSecured is called for both inbound and outbound connections,
// after a security handshake has taken place and we've authenticated the peer.
func (n *connGater) InterceptSecured(direction libp2pnetwork.Direction, id peer.ID, multiaddrs libp2pnetwork.ConnMultiaddrs) bool {
	return n.idx.IsBad(n.logger, id)
}

// InterceptUpgraded is called for inbound and outbound connections, after
// libp2p has finished upgrading the connection entirely to a secure,
// multiplexed channel.
func (n *connGater) InterceptUpgraded(conn libp2pnetwork.Conn) (bool, control.DisconnectReason) {
	return true, 0
}
