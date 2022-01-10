package p2p

import (
	libp2pnetwork "github.com/libp2p/go-libp2p-core/network"
	"github.com/libp2p/go-libp2p-core/peer"
	"go.uber.org/zap"
	"sync"
)

func (n *p2pNetwork) handleConnections() *libp2pnetwork.NotifyBundle {
	who := zap.String("who", "conn_handler")
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
		fieldPid := zap.String("peerID", pid)
		n.peersIndex.IndexConn(conn)
		if !n.peersIndex.Indexed(conn.RemotePeer()) {
			n.trace("connection was not indexed", fieldPid)
			// TODO: close connection in the future, currently might be an old peer or bootnode
			return
		}
		if !n.isPeerAtLimit(conn.Stat().Direction) {
			return
		}
		if !n.isRelevantPeer(id) {
			n.peersIndex.Prune(id)
			if err := net.ClosePeer(id); err != nil {
				n.trace("WARNING: could not close connection", fieldPid, zap.Error(err))
			}
		}
	}

	return &libp2pnetwork.NotifyBundle{
		ConnectedF: func(net libp2pnetwork.Network, conn libp2pnetwork.Conn) {
			if conn == nil || conn.RemoteMultiaddr() == nil {
				return
			}
			n.trace("connected peer", who,
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
			n.trace("disconnected peer", who,
				//zap.String("conn", conn.ID()),
				//zap.String("multiaddr", conn.RemoteMultiaddr().String()),
				zap.String("peerID", conn.RemotePeer().String()))
		},
	}
}

// isRelevantPeer checks if the current node should connect the given peer.
// a peer is relevant if it fullfils one of the following:
// - it shares a committee with the current node
// - it is an exporter or bootnode (TODO: bootnode)
func (n *p2pNetwork) isRelevantPeer(id peer.ID) bool {
	where := zap.String("where", "isRelevantPeer()")
	fieldPid := zap.String("peerID", id.String())
	//if !n.peersIndex.Indexed(id) {
	//	logger.Debug("peer was not indexed yet")
	//	return false
	//}
	oid, err := n.peersIndex.getOperatorID(id)
	if err != nil {
		n.trace("WARNING: could not read operator id", where, fieldPid, zap.Error(err))
		return false
	}
	if len(oid) > 0 {
		relevant := n.lookupOperator(oid)
		if !relevant {
			n.trace("operator is not relevant", where, zap.String("oid", oid), fieldPid)
		} else {
			n.trace("operator is relevant", where, zap.String("oid", oid), fieldPid)
		}
		return relevant
	}
	n.trace("could not find operator id, looking for node type", where, fieldPid)
	nodeType, err := n.peersIndex.getNodeType(id)
	if err != nil {
		n.trace("WARNING: could not read node type", where, fieldPid, zap.Error(err))
		return false
	}
	if nodeType == Operator {
		n.trace("WARNING: operator doesn't have an id", where, fieldPid)
		return false
	}
	// TODO: unmark once bootnode enr will include a type as well
	//if nodeType == Unknown {
	//	n.trace("WARNING: unknown peer", where, fieldPid)
	//	return false
	//}
	return true
}
