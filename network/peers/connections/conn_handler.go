package connections

import (
	"context"
	"time"

	"github.com/bloxapp/ssv/logging/fields"
	"github.com/bloxapp/ssv/network/peers"
	"github.com/bloxapp/ssv/network/records"
	"github.com/cornelk/hashmap"
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

	ongoingHandshakes := hashmap.New[peer.ID, struct{}]()
	acceptConnection := func(logger *zap.Logger, net libp2pnetwork.Network, conn libp2pnetwork.Conn) error {
		if _, ongoing := ongoingHandshakes.GetOrInsert(conn.RemotePeer(), struct{}{}); ongoing {
			// Another connection with the same peer is already being handled.
			return nil
		}
		defer func() {
			// Unset this peer as being handled.
			ongoingHandshakes.Del(conn.RemotePeer())
		}()

		pid := conn.RemotePeer()
		switch ch.peerInfos.State(pid) {
		case peers.StateConnected, peers.StateConnecting:
			logger.Debug("peer is already connected or connecting")
			return nil
		}
		ch.peerInfos.AddPeerInfo(pid, conn.RemoteMultiaddr(), conn.Stat().Direction)

		// Connection is inbound: wait for successful handshake request.
		if conn.Stat().Direction == network.DirInbound {
			startTime := time.Now()

			// Wait for peer to initiate handshake.
			time.Sleep(20 * time.Second)

			// Exit if we are disconnected with the peer.
			if net.Connectedness(pid) != network.Connected {
				return nil
			}

			// Disconnect if peer hasn't sent a handshake request.
			peerInfo := ch.peerInfos.PeerInfo(pid)
			if peerInfo == nil {
				return errors.New("failed to get PeerInfo")
			}
			if !peerInfo.LastHandshake.After(startTime) {
				return errors.New("peer hasn't sent a handshake request")
			}

			// Disconnect if handshake failed.
			if peerInfo.LastHandshakeError != nil {
				return errors.Wrap(peerInfo.LastHandshakeError, "peer failed handshake")
			}

			if !ch.sharesEnoughSubnets(logger, conn) {
				return errors.New("peer doesn't share enough subnets")
			}
			if ch.connIdx.Limit(conn.Stat().Direction) {
				return errors.New("reached peers limit")
			}
			return nil
		}

		// Connection is outbound: initiate handshake.
		ch.peerInfos.SetState(pid, peers.StateConnecting)
		err := ch.handshaker.Handshake(logger, conn)
		if err != nil {
			return errors.Wrap(err, "could not handshake")
		}
		if ch.connIdx.Limit(conn.Stat().Direction) {
			return errors.New("reached peers limit")
		}
		return nil
	}

	connLogger := func(conn libp2pnetwork.Conn) *zap.Logger {
		return logger.With(
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

			// Skip if we are still connected to the peer.
			if net.Connectedness(conn.RemotePeer()) == libp2pnetwork.Connected {
				return
			}

			metricsConnections.Dec()
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

	if mySubnets.String() == records.ZeroSubnets { // this node has no subnets
		return true
	}
	shared := records.SharedSubnets(mySubnets, subnets, 1)
	logger.Debug("checking subnets", zap.Ints("shared", shared))

	return len(shared) == 1
}
