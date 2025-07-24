package peers

import (
	"context"

	connmgrcore "github.com/libp2p/go-libp2p/core/connmgr"
	libp2pnetwork "github.com/libp2p/go-libp2p/core/network"
	"github.com/libp2p/go-libp2p/core/peer"
	"go.uber.org/zap"

	"github.com/ssvlabs/ssv/logging/fields"
	"github.com/ssvlabs/ssv/network/commons"
	s "github.com/ssvlabs/ssv/network/peers/scores"
)

// ConnManager is a wrapper on top of go-libp2p/core/connmgr.ConnManager.
// exposing an abstract interface so we can have the flexibility of doing some stuff manually
// rather than relaying on libp2p's connection manager.
type ConnManager interface {
	// TrimPeers will trim unprotected peers.
	TrimPeers(ctx context.Context, net libp2pnetwork.Network, peersToTrim map[peer.ID]struct{})
	// DisconnectFromBadPeers will disconnect from bad peers according to their Gossip scores. It returns the number of disconnected peers.
	DisconnectFromBadPeers(net libp2pnetwork.Network, allPeers []peer.ID) int
	// DisconnectFromIrrelevantPeers will disconnect from at most [disconnectQuota] peers that doesn't share any subnet in common. It returns the number of disconnected peers.
	DisconnectFromIrrelevantPeers(disconnectQuota int, net libp2pnetwork.Network, allPeers []peer.ID, mySubnets commons.Subnets) int
}

// connManager implements ConnManager
type connManager struct {
	logger           *zap.Logger
	connManager      connmgrcore.ConnManager
	subnetsIdx       SubnetsIndex
	gossipScoreIndex s.GossipScoreIndex
}

// NewConnManager creates a new conn manager.
// multiple instances can be created, but concurrency is not supported.
func NewConnManager(
	logger *zap.Logger,
	connMgr connmgrcore.ConnManager,
	subnetsIdx SubnetsIndex,
	gossipScoreIndex s.GossipScoreIndex,
) ConnManager {
	return &connManager{
		logger:           logger,
		connManager:      connMgr,
		subnetsIdx:       subnetsIdx,
		gossipScoreIndex: gossipScoreIndex,
	}
}

// Disconnects from a peer
func (c connManager) disconnect(peerID peer.ID, net libp2pnetwork.Network) error {
	return net.ClosePeer(peerID)
}

// TrimPeers closes the connection to peersToTrim.
func (c connManager) TrimPeers(
	ctx context.Context,
	net libp2pnetwork.Network,
	peersToTrim map[peer.ID]struct{},
) {
	for _, pid := range net.Peers() {
		if _, ok := peersToTrim[pid]; ok {
			if err := c.disconnect(pid, net); err != nil {
				c.logger.Error("error disconnecting from peer", fields.PeerID(pid), zap.Error(err))
			}
		}
	}
}

// DisconnectFromBadPeers will disconnect from bad peers according to their Gossip scores. It returns the number of disconnected peers.
func (c connManager) DisconnectFromBadPeers(net libp2pnetwork.Network, allPeers []peer.ID) int {
	disconnectedPeers := 0
	for _, peerID := range allPeers {
		// Disconnect if peer has bad gossip score.
		if isBad, gossipScore := c.gossipScoreIndex.HasBadGossipScore(peerID); isBad {
			err := c.disconnect(peerID, net)
			if err != nil {
				c.logger.Error("failed to disconnect from bad peer", fields.PeerID(peerID), zap.Float64("gossip_score", gossipScore))
			} else {
				c.logger.Debug("disconnecting from bad peer", fields.PeerID(peerID), zap.Float64("gossip_score", gossipScore))
				disconnectedPeers++
			}
		}
	}

	return disconnectedPeers
}

// DisconnectFromIrrelevantPeers will disconnect from at most [disconnectQuota] peers that doesn't share any subnet in common. It returns the number of disconnected peers.
func (c connManager) DisconnectFromIrrelevantPeers(disconnectQuota int, net libp2pnetwork.Network, allPeers []peer.ID, mySubnets commons.Subnets) int {
	disconnectedPeers := 0
	for _, peerID := range allPeers {
		var sharedSubnets []uint64
		peerSubnets, ok := c.subnetsIdx.GetPeerSubnets(peerID)
		if ok {
			sharedSubnets = mySubnets.SharedSubnets(peerSubnets)
		}

		// If there's no common subnet, disconnect from peer.
		if len(sharedSubnets) == 0 {
			err := c.disconnect(peerID, net)
			if err != nil {
				c.logger.Error("failed to disconnect from peer with irrelevant subnets", fields.PeerID(peerID))
			} else {
				c.logger.Debug("disconnecting from peer with irrelevant subnets", fields.PeerID(peerID))
				disconnectedPeers++
				if disconnectedPeers >= disconnectQuota {
					return disconnectedPeers
				}
			}
		}
	}
	return disconnectedPeers
}
