package connections

import (
	"context"
	"github.com/ssvlabs/ssv/network/discovery"
	"sync"
	"time"

	"github.com/libp2p/go-libp2p/core/network"
	libp2pnetwork "github.com/libp2p/go-libp2p/core/network"
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/pkg/errors"
	"go.uber.org/zap"

	"github.com/ssvlabs/ssv/logging"
	"github.com/ssvlabs/ssv/logging/fields"
	"github.com/ssvlabs/ssv/network/peers"
	"github.com/ssvlabs/ssv/network/records"
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
	metrics         Metrics
}

// NewConnHandler creates a new connection handler
func NewConnHandler(
	ctx context.Context,
	handshaker Handshaker,
	subnetsProvider SubnetsProvider,
	subnetsIndex peers.SubnetsIndex,
	connIdx peers.ConnectionIndex,
	peerInfos peers.PeerInfoIndex,
	mr Metrics,
) ConnHandler {
	return &connHandler{
		ctx:             ctx,
		handshaker:      handshaker,
		subnetsProvider: subnetsProvider,
		subnetsIndex:    subnetsIndex,
		connIdx:         connIdx,
		peerInfos:       peerInfos,
		metrics:         mr,
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

			// Handle the connection without blocking.
			go func() {
				logger := connLogger(conn)
				err := acceptConnection(logger, net, conn)
				if err == nil {
					if ch.connIdx.AtLimit(conn.Stat().Direction) {
						err = errors.New("reached peers limit")
					}
				}
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

				// see if this peer is already connected, we probably shouldn't encounter this often
				// if at all because this means we are creating duplicate (unnecessary) peer connections
				// and effectively reduce overall peer diversity (because we can't exceed pre-configured
				// max peer limit)
				discovery.ConnectedSubnets.Range(func(subnet int, ids []peer.ID) bool {
					for _, id := range ids {
						if id == conn.RemotePeer() {
							logger.Debug(
								"peer is already connected (through some subnet discovered previously)",
								zap.Int("subnet_id", subnet),
								zap.String("peer_id", string(id)),
							)
							return false // stop iteration, found what we are looking for
						}
					}
					return true
				})
				// see if we should upgrade any `discovered` subnets to `connected` through this peer
				//
				// unexpectedPeer helps us track whether the peer connection we are making is due
				// to us discovering this peer previously (due to looking for peers with subnets
				// we are interested in), or whether we are connecting to some "unexpected" peer
				// TODO - we should also account for `trustedPeers` here, I need to check how this
				// list is derived - but it seems to always be 0 (based on what logs report)
				unexpectedPeer := true
				discovery.DiscoveredSubnets.Range(func(subnet int, peerIDs []peer.ID) bool {
					otherPeers := make([]peer.ID, 0, len(peerIDs))
					for _, peerID := range peerIDs {
						if peerID == conn.RemotePeer() {
							discovery.Connected1stTimeSubnets.Get(subnet)
							_, ok := discovery.Connected1stTimeSubnets.Get(subnet)
							if !ok {
								logger.Debug(
									"connected subnet 1st time!",
									zap.Int("subnet_id", subnet),
									zap.String("peer_id", string(peerID)),
								)
								discovery.Connected1stTimeSubnets.Set(subnet, 1)
							}

							connectedPeers, _ := discovery.ConnectedSubnets.Get(subnet)
							// peerAlreadyContributesToSubnet helps us track and not double-count peer
							// contributions to subnets (discovery.ConnectedSubnets must contain unique
							// list of peers per subnet at any moment in time)
							peerAlreadyContributesToSubnet := false
							for _, connectedPeer := range connectedPeers {
								if connectedPeer == peerID {
									peerAlreadyContributesToSubnet = true
									break
								}
							}
							if !peerAlreadyContributesToSubnet {
								logger.Debug(
									"connected contributing peer (since he has a subnet we are interested in - as we discovered previously)",
									zap.Int("subnet_id", subnet),
									zap.String("peer_id", string(peerID)),
								)
								connectedPeers = append(connectedPeers, peerID)
								discovery.ConnectedSubnets.Set(subnet, connectedPeers)
							}
							continue
						}
						otherPeers = append(otherPeers, peerID)
					}

					if len(otherPeers) == len(peerIDs) {
						// this discovered subnet is not related to connected peer
						return true
					}

					unexpectedPeer = false // this peer connection is happening due to our subnet discovery process

					// exclude this peer from discovered list since we've just connected to him, this
					// limits DiscoveredSubnets map to only those discovered peers whom we didn't/couldn't
					// connect to yet - this means DiscoveredSubnets map shouldn't grow big for ANY of the
					// subnets it contains because that would mean (for such a subnet) we are discovering
					// peers but don't connect to them for some reason.
					discovery.DiscoveredSubnets.Set(subnet, otherPeers)

					return true
				})
				if unexpectedPeer {
					logger.Debug(
						"connected peer that doesn't belong to ANY of recently discovered subnets",
						zap.String("peer_id", string(conn.RemotePeer())),
					)
				}
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
			ch.metrics.PeerDisconnected(conn.RemotePeer())

			logger := connLogger(conn)
			logger.Debug("peer disconnected")

			// unexpectedPeer helps us track whether the peer we've disconnected is a peer
			// that's been connected previously through the process of subnet-discovery,
			// it's just a sanity check
			// TODO - we should also account for `trustedPeers` here, I need to check how this
			// list is derived - but it seems to always be 0 (based on what logs report)
			unexpectedPeer := true
			discovery.ConnectedSubnets.Range(func(subnet int, peerIDs []peer.ID) bool {
				otherPeers := make([]peer.ID, 0, len(peerIDs))
				for _, peerID := range peerIDs {
					if peerID == conn.RemotePeer() {
						continue
					}
					otherPeers = append(otherPeers, peerID)
				}

				if len(otherPeers) == len(peerIDs) {
					// this subnet was not affected by disconnected peer
					return true
				}

				unexpectedPeer = false // this peer disconnect is happening due to our subnet discovery process

				// exclude this peer from connected list since we've just disconnected him, this
				// limits ConnectedSubnets map to only those peers whom we still have active connection
				// with - this means ConnectedSubnets map shouldn't grow small for ANY of the
				// subnets it contains because that would mean (for such a subnet) we are getting
				// close to killing previously alive subnet.
				discovery.ConnectedSubnets.Set(subnet, otherPeers)

				// also, put this peer back into DiscoveredSubnets because he is a potential candidate
				// we might consider connecting to in the future.
				// TODO - this means DiscoveredSubnets might not be 100% accurate (it might claim that
				// certain peers are connected to subnets they no longer are, but it should be a rare
				// case - for peer to advertise a subnet and no longer is interested in it)
				discoveredPeers, _ := discovery.DiscoveredSubnets.Get(subnet)
				peerAlreadyDiscoveredForSubnet := false
				for _, peerID := range discoveredPeers {
					if peerID == conn.RemotePeer() {
						// TODO - it's fine to get this warning occasionally, I guess ? Not sure what
						// it would mean though ... a peer who has advertised he has a subnet, but then
						// we discovered that he doesn't, and then peer says (still/again) that does
						// work with that subnet ...
						logger.Debug(
							"already discovered this subnet through this peer",
							zap.Int("subnet_id", subnet),
							zap.String("peer_id", string(peerID)),
						)
						peerAlreadyDiscoveredForSubnet = true
						break
					}
				}
				if !peerAlreadyDiscoveredForSubnet {
					discovery.DiscoveredSubnets.Set(subnet, append(discoveredPeers, conn.RemotePeer()))
				}

				if len(otherPeers) == 1 {
					logger.Debug(
						"disconnecting peer resulted in Solo subnet",
						zap.Int("subnet_id", subnet),
						zap.String("peer_id", string(conn.RemotePeer())),
					)
				}
				if len(otherPeers) == 0 {
					logger.Debug(
						"disconnecting peer resulted in Dead subnet",
						zap.Int("subnet_id", subnet),
						zap.String("peer_id", string(conn.RemotePeer())),
					)
				}

				return true
			})
			if unexpectedPeer {
				logger.Debug(
					"disconnected peer that doesn't belong to ANY of connected subnets",
					zap.String("peer_id", string(conn.RemotePeer())),
				)
			}
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
