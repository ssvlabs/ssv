package p2p

import (
	"github.com/libp2p/go-libp2p-core/control"
	libp2pnetwork "github.com/libp2p/go-libp2p-core/network"
	"github.com/libp2p/go-libp2p-core/peer"
	ma "github.com/multiformats/go-multiaddr"
	"go.uber.org/zap"
)

// InterceptPeerDial is called on an imminent outbound peer dial request, prior
// to the addresses of that peer being available/resolved. Blocking connections
// at this stage is typical for blacklisting scenarios
func (n *p2pNetwork) InterceptPeerDial(id peer.ID) bool {
	n.logger.Debug("conn_gater: InterceptPeerDial", zap.String("pid", id.String()))
	return !n.isPeerBlacklisted(id)
}

// InterceptAddrDial is called on an imminent outbound dial to a peer on a
// particular address. Blocking connections at this stage is typical for
// address filtering.
func (n *p2pNetwork) InterceptAddrDial(id peer.ID, multiaddr ma.Multiaddr) bool {
	n.logger.Debug("conn_gater: InterceptAddrDial", zap.String("pid", id.String()))
	return !n.isAddrBlacklisted(id, multiaddr)
}

// InterceptAccept is called as soon as a transport listener receives an
// inbound connection request, before any upgrade takes place. Transports who
// accept already secure and/or multiplexed connections (e.g. possibly QUIC)
// MUST call this method regardless, for correctness/consistency.
func (n *p2pNetwork) InterceptAccept(multiaddrs libp2pnetwork.ConnMultiaddrs) bool {
	n.logger.Debug("conn_gater: InterceptAccept")
	return true
}

// InterceptSecured is called for both inbound and outbound connections,
// after a security handshake has taken place and we've authenticated the peer.
//
func (n *p2pNetwork) InterceptSecured(direction libp2pnetwork.Direction, id peer.ID, multiaddrs libp2pnetwork.ConnMultiaddrs) bool {
	n.logger.Debug("conn_gater: InterceptSecured", zap.String("pid", id.String()))
	return true
}

// InterceptUpgraded is called for inbound and outbound connections, after
// libp2p has finished upgrading the connection entirely to a secure,
// multiplexed channel.
//
// It checks whether we reached peers limit, if we do, accept connection only for relevant peers
func (n *p2pNetwork) InterceptUpgraded(conn libp2pnetwork.Conn) (bool, control.DisconnectReason) {
	id := conn.RemotePeer()
	n.logger.Debug("conn_gater: InterceptUpgraded", zap.String("pid", id.String()))
	//if n.isPeerAtLimit(conn.Stat().Direction) {
	if !n.peersIndex.Indexed(id) {
		n.logger.Debug("conn_gater: not indexed", zap.String("pid", id.String()))
		n.peersIndex.IndexConn(conn)
		if !n.peersIndex.Indexed(id) {
			n.logger.Warn("could not index connection", zap.String("pid", id.String()))
		}
		n.logger.Debug("conn_gater: indexed", zap.String("pid", id.String()))
	}
	relevant := n.isRelevantPeer(id)
	//if relevant {
	//	n.host.ConnManager().Protect(id, "ssv-peer")
	//}
	n.logger.Debug("conn_gater: isRelevant", zap.Bool("relevant", relevant), zap.String("pid", id.String()))
	if n.isPeerAtLimit(conn.Stat().Direction) {
		return relevant, 0
	}
	return true, 0
}

// isRelevantPeer checks if the current node should connect the given peer.
// returns whether the peer is relevant and indexed.
// a peer is relevant if it fullfils one of the following:
// - it shares a committee with the current node
// - it is an exporter node
func (n *p2pNetwork) isRelevantPeer(id peer.ID) bool {
	logger := n.logger.With(zap.String("pid", id.String()))
	if !n.peersIndex.Indexed(id) {
		logger.Debug("peer was not indexed yet")
		return false
	}
	oid, err := n.peersIndex.getOperatorID(id)
	if err != nil {
		logger.Warn("could not read operator id", zap.Error(err))
		return false
	}
	if len(oid) > 0 {
		return n.lookupHandler(oid)
	}
	logger.Debug("could not find operator id")
	nodeType, err := n.peersIndex.getNodeType(id)
	if err != nil {
		logger.Warn("could not read node type", zap.Error(err))
		return false
	}
	// TODO: change to `nodeType != Exporter` once enough operators are on >=v0.1.9 where the ENR entry (`oid`) was be added, currently accepting old nodes
	if nodeType == Operator {
		n.logger.Debug("operator doesn't have an id")
		return false
	}
	return true
}

// isPeerBlacklisted checks if the given peer is blacklisted
func (n *p2pNetwork) isPeerBlacklisted(id peer.ID) bool {
	// TODO: implement filtering by peer IDs
	return false
}

// isAddrBlacklisted checks if the given address is blacklisted
func (n *p2pNetwork) isAddrBlacklisted(id peer.ID, multiaddr ma.Multiaddr) bool {
	// TODO: implement filtering of addresses
	return false
}
