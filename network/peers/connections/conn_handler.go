package connections

import (
	"context"
	"sync"
	"time"

	libp2pnetwork "github.com/libp2p/go-libp2p/core/network"
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/pkg/errors"
	"go.uber.org/zap"

	"github.com/ssvlabs/ssv/logging"
	"github.com/ssvlabs/ssv/logging/fields"
	"github.com/ssvlabs/ssv/network/commons"
	"github.com/ssvlabs/ssv/network/discovery"
	"github.com/ssvlabs/ssv/network/peers"
	"github.com/ssvlabs/ssv/utils/ttl"
)

// ConnHandler handles new connections (inbound / outbound) using libp2pnetwork.NotifyBundle
type ConnHandler interface {
	Handle(logger *zap.Logger) *libp2pnetwork.NotifyBundle
}

// connHandler implements ConnHandler
type connHandler struct {
	ctx context.Context

	handshaker          Handshaker
	subnetsProvider     SubnetsProvider
	subnetsIndex        peers.SubnetsIndex
	connIdx             peers.ConnectionIndex
	peerInfos           peers.PeerInfoIndex
	discoveredPeersPool *ttl.Map[peer.ID, discovery.DiscoveredPeer]
}

// NewConnHandler creates a new connection handler
func NewConnHandler(
	ctx context.Context,
	handshaker Handshaker,
	subnetsProvider SubnetsProvider,
	subnetsIndex peers.SubnetsIndex,
	connIdx peers.ConnectionIndex,
	peerInfos peers.PeerInfoIndex,
	discoveredPeersPool *ttl.Map[peer.ID, discovery.DiscoveredPeer],
) ConnHandler {
	return &connHandler{
		ctx:                 ctx,
		handshaker:          handshaker,
		subnetsProvider:     subnetsProvider,
		subnetsIndex:        subnetsIndex,
		connIdx:             connIdx,
		peerInfos:           peerInfos,
		discoveredPeersPool: discoveredPeersPool,
	}
}

// Handle configures a network notifications handler that handshakes and tracks all p2p connections
func (ch *connHandler) Handle(logger *zap.Logger) *libp2pnetwork.NotifyBundle {
	disconnect := func(logger *zap.Logger, net libp2pnetwork.Network, conn libp2pnetwork.Conn) {
		id := conn.RemotePeer()
		errClose := net.ClosePeer(id)
		if errClose == nil {
			recordFiltered(ch.ctx, conn.Stat().Direction)
		}
	}

	ongoingHandshakes := map[peer.ID]struct{}{}
	ongoingHandshakesMutex := &sync.Mutex{}
	beginHandshake := func(pid peer.ID) bool {
		ongoingHandshakesMutex.Lock()
		defer ongoingHandshakesMutex.Unlock()
		if _, ongoing := ongoingHandshakes[pid]; ongoing {
			return false
		}
		ongoingHandshakes[pid] = struct{}{}
		return true
	}
	endHandshake := func(pid peer.ID) {
		ongoingHandshakesMutex.Lock()
		defer ongoingHandshakesMutex.Unlock()
		delete(ongoingHandshakes, pid)
	}

	var ignoredConnection = errors.New("ignored connection")
	acceptConnection := func(logger *zap.Logger, net libp2pnetwork.Network, conn libp2pnetwork.Conn) (err error) {
		defer func() {
			if r := recover(); r != nil {
				err = errors.Errorf("panic: %v", r)
			}
		}()

		pid := conn.RemotePeer()

		if !beginHandshake(pid) {
			// Another connection with the same peer is already being handled.
			logger.Debug("peer is already being handled")
			return ignoredConnection
		}
		defer func() {
			// Unset this peer as being handled.
			endHandshake(pid)
		}()

		switch ch.peerInfos.State(pid) {
		case peers.StateConnected, peers.StateConnecting:
			logger.Debug("peer is already connected or connecting")
			return ignoredConnection
		}
		ch.peerInfos.AddPeerInfo(pid, conn.RemoteMultiaddr(), conn.Stat().Direction)

		// Connection is inbound -> Wait for successful handshake request.
		if conn.Stat().Direction == libp2pnetwork.DirInbound {
			// Wait for peer to initiate handshake.
			logger.Debug("waiting for peer to initiate handshake")
			start := time.Now()
			deadline := time.NewTimer(20 * time.Second)
			ticker := time.NewTicker(1 * time.Second)
			defer deadline.Stop()
			defer ticker.Stop()
		Wait:
			for {
				select {
				case <-deadline.C:
					return errors.New("peer hasn't sent a handshake request")
				case <-ticker.C:
					// Check if peer has sent a handshake request.
					if pi := ch.peerInfos.PeerInfo(pid); pi != nil && pi.LastHandshake.After(start) {
						if pi.LastHandshakeError != nil {
							// Handshake failed.
							return errors.Wrap(pi.LastHandshakeError, "peer failed handshake")
						}

						// Handshake succeeded.
						break Wait
					}

					if net.Connectedness(pid) != libp2pnetwork.Connected {
						return errors.New("lost connection")
					}
				}
			}

			if !ch.sharesEnoughSubnets(logger, conn) {
				return errors.New("peer doesn't share enough subnets")
			}

			return nil
		}

		// Connection is outbound -> Initiate handshake.
		logger.Debug("initiating handshake")
		ch.peerInfos.SetState(pid, peers.StateConnecting)
		if err := ch.handshaker.Handshake(logger, conn); err != nil {
			return errors.Wrap(err, "could not handshake")
		}
		return nil
	}

	connLogger := func(conn libp2pnetwork.Conn) *zap.Logger {
		return logger.Named(logging.NameConnHandler).
			With(
				fields.PeerID(conn.RemotePeer()),
				zap.String("remote_addr", conn.RemoteMultiaddr().String()),
				zap.String("conn_dir", conn.Stat().Direction.String()),
			)
	}
	return &libp2pnetwork.NotifyBundle{
		ConnectedF: func(net libp2pnetwork.Network, conn libp2pnetwork.Conn) {
			if conn == nil || conn.RemoteMultiaddr() == nil {
				return
			}

			// Handle the connection (could be either incoming or outgoing) without blocking.
			go func() {
				logger := connLogger(conn)
				err := acceptConnection(logger, net, conn)
				if err == nil {
					if ch.connIdx.AtLimit(conn.Stat().Direction) {
						err = errors.New("reached total connected peers limit")
					}
				}
				if errors.Is(err, ignoredConnection) {
					return
				}
				if err != nil {
					disconnect(logger, net, conn)
					logger.Debug("not gonna connect this peer", zap.Error(err))
					return
				}

				// Successfully connected.
				recordConnected(ch.ctx, conn.Stat().Direction)

				ch.peerInfos.SetState(conn.RemotePeer(), peers.StateConnected)
				logger.Debug("peer connected")

				// if this connection is the one we found through discovery - remove it from discoveredPeersPool
				// so we won't be retrying connecting that same peer again until discovery stumbles upon it again
				// (discovery also filters out peers we are already connected to, meaning it should re-discover
				// that same peer only after we'll disconnect him). Note, this is best-effort solution meaning
				// we still might try connecting to this peer even though we've connected him here - this is
				// because it would be hard to implement the prevention for this that works atomically
				ch.discoveredPeersPool.Delete(conn.RemotePeer())
			}()
		},
		DisconnectedF: func(net libp2pnetwork.Network, conn libp2pnetwork.Conn) {
			if conn == nil || conn.RemoteMultiaddr() == nil {
				return
			}

			// Skip if we are still connected to the peer.
			if net.Connectedness(conn.RemotePeer()) == libp2pnetwork.Connected {
				return
			}

			recordDisconnected(ch.ctx, conn.Stat().Direction)

			ch.peerInfos.SetState(conn.RemotePeer(), peers.StateDisconnected)

			logger := connLogger(conn)
			logger.Debug("peer disconnected")
		},
	}
}

func (ch *connHandler) sharesEnoughSubnets(logger *zap.Logger, conn libp2pnetwork.Conn) bool {
	pid := conn.RemotePeer()
	subnets := ch.subnetsIndex.GetPeerSubnets(pid)
	if len(subnets) == 0 {
		// no subnets for this peer
		return false
	}
	mySubnets := ch.subnetsProvider()

	logger = logger.With(fields.Subnets(subnets), zap.String("my_subnets", mySubnets.String()))

	if mySubnets.String() == commons.ZeroSubnets { // this node has no subnets
		return true
	}
	shared := commons.SharedSubnets(mySubnets, subnets, 1)
	logger.Debug("checking subnets", zap.Ints("shared", shared))

	return len(shared) == 1
}
