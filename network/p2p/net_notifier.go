package p2p

import (
	libp2pnetwork "github.com/libp2p/go-libp2p-core/network"
	"go.uber.org/zap"
)

func (n *p2pNetwork) notifier() *libp2pnetwork.NotifyBundle {
	// TODO: add connection state management
	return &libp2pnetwork.NotifyBundle{
		ConnectedF: func(net libp2pnetwork.Network, conn libp2pnetwork.Conn) {
			if conn == nil || conn.RemoteMultiaddr() == nil {
				return
			}
			n.trace("connected peer", zap.String("who", "networkNotifiee"),
				zap.String("conn", conn.ID()),
				zap.String("multiaddr", conn.RemoteMultiaddr().String()),
				zap.String("peerID", conn.RemotePeer().String()))
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
				zap.String("conn", conn.ID()),
				zap.String("multiaddr", conn.RemoteMultiaddr().String()),
				zap.String("peerID", conn.RemotePeer().String()))
		},
	}
}
