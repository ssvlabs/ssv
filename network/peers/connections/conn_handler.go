package connections

import (
	"context"
	"sync"
	"time"

	"github.com/bloxapp/ssv/logging"
	"github.com/bloxapp/ssv/logging/fields"
	"github.com/bloxapp/ssv/network/peers"
	"github.com/bloxapp/ssv/network/records"
	"github.com/libp2p/go-libp2p/core/network"
	libp2pnetwork "github.com/libp2p/go-libp2p/core/network"
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/pkg/errors"
	"go.uber.org/zap"
)

// ConnHandler handles new connections (inbound / outbound) using libp2pnetwork.NotifyBundle
type ConnHandler interface {
	Handle(logger *zap.Logger) *libp2pnetwork.NotifyBundle
}

// connHandler implements ConnHandler
type connHandler struct {
	ctx context.Context

	handshaker      Handshaker
	subnetsProvider SubnetsProvider
	subnetsIndex    peers.SubnetsIndex
	connIdx         peers.ConnectionIndex
	peerInfos       peers.PeerInfoIndex
}

// NewConnHandler creates a new connection handler
func NewConnHandler(ctx context.Context, handshaker Handshaker, subnetsProvider SubnetsProvider, subnetsIndex peers.SubnetsIndex, connIdx peers.ConnectionIndex, peerInfos peers.PeerInfoIndex) ConnHandler {
	return &connHandler{
		ctx:             ctx,
		handshaker:      handshaker,
		subnetsProvider: subnetsProvider,
		subnetsIndex:    subnetsIndex,
		connIdx:         connIdx,
		peerInfos:       peerInfos,
	}
}

// Handle configures a network notifications handler that handshakes and tracks all p2p connections
func (ch *connHandler) Handle(logger *zap.Logger) *libp2pnetwork.NotifyBundle {
	disconnect := func(logger *zap.Logger, net libp2pnetwork.Network, conn libp2pnetwork.Conn) {
		id := conn.RemotePeer()
		errClose := net.ClosePeer(id)
		if errClose == nil {
			metricsFilteredConnections.Inc()
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
	acceptConnection := func(logger *zap.Logger, net libp2pnetwork.Network, conn libp2pnetwork.Conn) error {
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
		if conn.Stat().Direction == network.DirInbound {
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

					if net.Connectedness(pid) != network.Connected {
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
		err := ch.handshaker.Handshake(logger, conn)
		if err != nil {
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

			// Handle the connection without blocking.
			go func() {
				logger := connLogger(conn)
				err := acceptConnection(logger, net, conn)
				// if err == nil {
				// 	if ch.connIdx.Limit(conn.Stat().Direction) {
				// 		err = errors.New("reached peers limit")
				// 	}
				// }
				if errors.Is(err, ignoredConnection) {
					return
				}
				if err != nil {
					disconnect(logger, net, conn)
					logger.Debug("failed to accept connection", zap.Error(err))
					return
				}

				// Successfully connected.
				metricsConnections.Inc()
				ch.peerInfos.SetState(conn.RemotePeer(), peers.StateConnected)
				logger.Debug("peer connected")
			}()
		},
		DisconnectedF: func(net libp2pnetwork.Network, conn libp2pnetwork.Conn) {
			if conn == nil || conn.RemoteMultiaddr() == nil {
				return
			}
			// Must be handled in a goroutine as this callback cannot be blocking.
			go func() {
				// Skip if we are still connected to the peer.
				if net.Connectedness(conn.RemotePeer()) == libp2pnetwork.Connected {
					logger.Debug("peer disconnected - we are still connected")
					return
				}

				metricsConnections.Dec()
				ch.peerInfos.SetState(conn.RemotePeer(), peers.StateDisconnected)

				logger := connLogger(conn)
				logger.Debug("peer disconnected")
			}()
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

	if mySubnets.String() == records.ZeroSubnets { // this node has no subnets
		return true
	}
	shared := records.SharedSubnets(mySubnets, subnets, 1)
	logger.Debug("checking subnets", zap.Ints("shared", shared))

	return len(shared) == 1
}
