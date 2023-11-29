package connections

import (
	"time"

	"github.com/bloxapp/ssv/network/peers"
	"github.com/imkira/go-ttlmap"
	"github.com/libp2p/go-libp2p/core/control"
	libp2pnetwork "github.com/libp2p/go-libp2p/core/network"
	"github.com/libp2p/go-libp2p/core/peer"
	ma "github.com/multiformats/go-multiaddr"
	"go.uber.org/zap"
)

// connGater implements ConnectionGater interface:
// https://github.com/libp2p/go-libp2p/blob/master/core/connmgr/gater.go
// TODO: add IP limiting

type ConnectionGater struct {
	logger            *zap.Logger
	idx               peers.ConnectionIndex
	blackList         *ttlmap.Map
	blackListDuration time.Duration
}

// NewConnectionGater creates a new instance of ConnectionGater
func NewConnectionGater(logger *zap.Logger, capacity int, blackListDuration time.Duration) *ConnectionGater {
	return &ConnectionGater{
		logger:            logger,
		blackListDuration: blackListDuration,
		blackList: ttlmap.New(&ttlmap.Options{
			InitialCapacity: capacity}),
	}
}

func (n *ConnectionGater) SetPeerIndex(idx peers.ConnectionIndex) {
	n.idx = idx
}

func (n *ConnectionGater) BlockPeer(id peer.ID) {
	err := n.blackList.Set(id.String(), ttlmap.NewItem(struct{}{}, ttlmap.WithTTL(n.blackListDuration)), nil)
	if err != nil {
		n.logger.Debug("can not blacklist peer")
	}
}

func (n *ConnectionGater) IsPeerBlocked(id peer.ID) bool {
	_, err := n.blackList.Get(id.String())
	return err == nil
}

// InterceptPeerDial is called on an imminent outbound peer dial request, prior
// to the addresses of that peer being available/resolved. Blocking connections
// at this stage is typical for blacklisting scenarios
func (n *ConnectionGater) InterceptPeerDial(id peer.ID) bool {
	if n.IsPeerBlocked(id) {
		return false
	}
	if n.idx == nil {
		return false
	}
	if n.idx.Limit(libp2pnetwork.DirOutbound) {
		n.logger.Debug("Not accepting outbound dial", zap.String("peer", id.String()),
			zap.String("reason", "at peer limit"))
		return false
	}

	return true
}

// InterceptAddrDial is called on an imminent outbound dial to a peer on a
// particular address. Blocking connections at this stage is typical for
// address filtering.
func (n *ConnectionGater) InterceptAddrDial(id peer.ID, multiaddr ma.Multiaddr) bool {
	return true
}

// InterceptAccept is called as soon as a transport listener receives an
// inbound connection request, before any upgrade takes place. Transports who
// accept already secure and/or multiplexed connections (e.g. possibly QUIC)
// MUST call this method regardless, for correctness/consistency.
func (n *ConnectionGater) InterceptAccept(multiaddrs libp2pnetwork.ConnMultiaddrs) bool {
	if n.idx == nil {
		return false
	}
	if n.idx.Limit(libp2pnetwork.DirInbound) {
		n.logger.Debug("Not accepting inbound dial", zap.String("remote_addr", multiaddrs.RemoteMultiaddr().String()),
			zap.String("reason", "at peer limit"))
		return false
	}

	return true
}

// InterceptSecured is called for both inbound and outbound connections,
// after a security handshake has taken place and we've authenticated the peer.
func (n *ConnectionGater) InterceptSecured(direction libp2pnetwork.Direction, id peer.ID, multiaddrs libp2pnetwork.ConnMultiaddrs) bool {
	if n.IsPeerBlocked(id) {
		return false
	}
	if n.idx == nil {
		return false
	}
	return !n.idx.IsBad(n.logger, id)
}

// InterceptUpgraded is called for inbound and outbound connections, after
// libp2p has finished upgrading the connection entirely to a secure,
// multiplexed channel.
func (n *ConnectionGater) InterceptUpgraded(conn libp2pnetwork.Conn) (bool, control.DisconnectReason) {
	return true, 0
}
