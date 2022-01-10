package p2p

import (
	libp2pnetwork "github.com/libp2p/go-libp2p-core/network"
	"github.com/libp2p/go-libp2p-core/peer"
	"go.uber.org/zap"
	"sync"
)

func (n *p2pNetwork) handleConnections() *libp2pnetwork.NotifyBundle {
	// TODO: add connection state management
	pending := make(map[string]bool)
	mut := sync.Mutex{}

	addPending := func(pid string) bool {
		mut.Lock()
		defer mut.Unlock()
		if pending[pid] {
			return false
		}
		pending[pid] = true
		return true
	}

	removePending := func(pid string) {
		mut.Lock()
		defer mut.Unlock()
		delete(pending, pid)
	}

	handleNewConnection := func(net libp2pnetwork.Network, conn libp2pnetwork.Conn) {
		id := conn.RemotePeer()
		pid := id.String()
		if !addPending(pid) {
			// connection to this peer is already handled
			return
		}
		defer removePending(pid)
		logger := n.logger.With(zap.String("who", "p2p/networkNotifiee"), zap.String("peerID", pid))
		n.peersIndex.IndexConn(conn)
		if !n.peersIndex.Indexed(conn.RemotePeer()) {
			logger.Warn("connection was not indexed")
			return
		}
		if !n.isPeerAtLimit(conn.Stat().Direction) {
			return
		}
		if !n.isRelevantPeer(id) {
			logger.Warn("pruning irrelevant connection")
			n.peersIndex.Prune(conn.RemotePeer())
			if err := net.ClosePeer(conn.RemotePeer()); err != nil {
				logger.Warn("could not close connection", zap.Error(err))
			}
		}
	}

	return &libp2pnetwork.NotifyBundle{
		ConnectedF: func(net libp2pnetwork.Network, conn libp2pnetwork.Conn) {
			if conn == nil || conn.RemoteMultiaddr() == nil {
				return
			}
			n.trace("connected peer", zap.String("who", "networkNotifiee"),
				//	zap.String("conn", conn.ID()),
				//	zap.String("multiaddr", conn.RemoteMultiaddr().String()),
				zap.String("peerID", conn.RemotePeer().String()))
			go handleNewConnection(net, conn)
		},
		DisconnectedF: func(net libp2pnetwork.Network, conn libp2pnetwork.Conn) {
			if conn == nil || conn.RemoteMultiaddr() == nil {
				return
			}
			// skip if we are still connected to the peer
			if net.Connectedness(conn.RemotePeer()) == libp2pnetwork.Connected {
				return
			}
			n.trace("disconnected peer", zap.String("who", "networkNotifiee"),
				//zap.String("conn", conn.ID()),
				//zap.String("multiaddr", conn.RemoteMultiaddr().String()),
				zap.String("peerID", conn.RemotePeer().String()))
		},
	}
}

// isRelevantPeer checks if the current node should connect the given peer.
// returns whether the peer is relevant and indexed.
// a peer is relevant if it fullfils one of the following:
// - it shares a committee with the current node
// - it is an exporter node
func (n *p2pNetwork) isRelevantPeer(id peer.ID) bool {
	logger := n.logger.With(zap.String("pid", id.String()))
	//if !n.peersIndex.Indexed(id) {
	//	logger.Debug("peer was not indexed yet")
	//	return false
	//}
	oid, err := n.peersIndex.getOperatorID(id)
	if err != nil {
		logger.Warn("could not read operator id", zap.Error(err))
		return false
	}
	if len(oid) > 0 {
		relevant := n.lookupHandler(oid)
		if !relevant {
			logger.Debug("operator is not relevant", zap.String("oid", oid))
		} else {
			logger.Debug("operator is relevant", zap.String("oid", oid))
		}
		return relevant
	}
	logger.Debug("could not find operator id")
	nodeType, err := n.peersIndex.getNodeType(id)
	if err != nil {
		logger.Warn("could not read node type", zap.Error(err))
		return false
	}
	if nodeType == Operator {
		n.logger.Debug("operator doesn't have an id")
		return false
	}
	// TODO: unmark once bootnode enr will include a type as well
	//if nodeType == Unknown {
	//	n.logger.Debug("unknown peer")
	//	return false
	//}
	return true
}
