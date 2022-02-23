package p2p

import (
	libp2pnetwork "github.com/libp2p/go-libp2p-core/network"
	"go.uber.org/zap"
	"sync"
)

// ConnectionFilter represents a function that filters connections.
// returns whether to close the connection and if some error was thrown
type ConnectionFilter func(conn libp2pnetwork.Conn) (bool, error)

// handleConnections accepts filters to be called upon requests
// configures a network notifications handler that indexes and filters all p2p connections
func (n *p2pNetwork) handleConnections(filters ...ConnectionFilter) *libp2pnetwork.NotifyBundle {
	who := zap.String("who", "conn_handler")
	// TODO: add connection state management
	pending := make(map[string]bool)
	pendingLock := sync.Mutex{}

	addPending := func(pid string) bool {
		pendingLock.Lock()
		defer pendingLock.Unlock()
		if pending[pid] {
			return false
		}
		pending[pid] = true
		return true
	}

	removePending := func(pid string) {
		pendingLock.Lock()
		defer pendingLock.Unlock()
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

		reportConnection(true)

		fieldPid := zap.String("peerID", pid)
		n.peersIndex.IndexConn(conn)
		for _, f := range filters {
			ok, err := f(conn)
			if err != nil {
				n.trace("WARNING: failed to run filter", fieldPid, zap.Error(err))
				return
			}
			if !ok {
				if err := net.ClosePeer(id); err != nil {
					n.trace("WARNING: could not close connection", fieldPid, zap.Error(err))
				}
				return
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
			reportConnection(false)
			n.trace("disconnected peer", who,
				//zap.String("conn", conn.ID()),
				//zap.String("multiaddr", conn.RemoteMultiaddr().String()),
				zap.String("peerID", conn.RemotePeer().String()))
		},
		OpenedStreamF: func(network libp2pnetwork.Network, stream libp2pnetwork.Stream) {
			if conn := stream.Conn(); conn != nil {
				reportStream(true)
				n.trace("new stream opened", zap.String("peerID", conn.RemotePeer().String()))
			}
		},
		ClosedStreamF: func(network libp2pnetwork.Network, stream libp2pnetwork.Stream) {
			if conn := stream.Conn(); conn != nil {
				reportStream(false)
				n.trace("stream closed", zap.String("peerID", conn.RemotePeer().String()))
			}
		},
	}
}
