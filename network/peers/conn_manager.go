package peers

import (
	"context"
	"time"

	"github.com/jellydator/ttlcache/v3"
	connmgrcore "github.com/libp2p/go-libp2p/core/connmgr"
	libp2pnetwork "github.com/libp2p/go-libp2p/core/network"
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/ssvlabs/ssv/logging/fields"
	"github.com/ssvlabs/ssv/network/records"
	"go.uber.org/zap"
)

const (
	ProtectedTag = "ssv/subnets"
)

type PeerScore float64

type DiscoveredPeer struct {
	peer.AddrInfo
	// ConnectRetries keeps track of how many times we tried to connect to this peer.
	ConnectRetries int
}

var (
	// DiscoveredPeersPool keeps track of recently discovered peers so we can rank them and choose
	// the best candidates to connect to.
	//DiscoveredPeersPool = ttlcache.New(ttlcache.WithTTL[peer.ID, DiscoveredPeer](30 * time.Minute))
	// TODO - for debugging, can remove later
	DiscoveredPeersPool = ttlcache.New(ttlcache.WithTTL[peer.ID, DiscoveredPeer](120 * time.Minute))
	TrimmedRecently     = ttlcache.New(ttlcache.WithTTL[peer.ID, struct{}](30 * time.Minute))
)

func init() {
	go TrimmedRecently.Start()     // start cleanup go-routine
	go DiscoveredPeersPool.Start() // start cleanup go-routine
}

// ConnManager is a wrapper on top of go-libp2p/core/connmgr.ConnManager.
// exposing an abstract interface so we can have the flexibility of doing some stuff manually
// rather than relaying on libp2p's connection manager.
type ConnManager interface {
	// TrimPeers will trim unprotected peers.
	TrimPeers(ctx context.Context, logger *zap.Logger, net libp2pnetwork.Network, maxTrims int)
	// DisconnectFromBadPeers will disconnect from bad peers according to their Gossip scores. It returns the number of disconnected peers.
	DisconnectFromBadPeers(logger *zap.Logger, net libp2pnetwork.Network, allPeers []peer.ID) int
	// DisconnectFromIrrelevantPeers will disconnect from at most [disconnectQuota] peers that doesn't share any subnet in common. It returns the number of disconnected peers.
	DisconnectFromIrrelevantPeers(logger *zap.Logger, disconnectQuota int, net libp2pnetwork.Network, allPeers []peer.ID, mySubnets records.Subnets) int
}

// connManager implements ConnManager
type connManager struct {
	logger           *zap.Logger
	connManager      connmgrcore.ConnManager
	subnetsIdx       SubnetsIndex
	gossipScoreIndex GossipScoreIndex
}

// NewConnManager creates a new conn manager.
// multiple instances can be created, but concurrency is not supported.
func NewConnManager(logger *zap.Logger, connMgr connmgrcore.ConnManager, subnetsIdx SubnetsIndex, gossipScoreIndex GossipScoreIndex) ConnManager {
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

// TrimPeers closes the connection to all peers that are not protected, dropping up to maxTrims peers.
func (c connManager) TrimPeers(ctx context.Context, logger *zap.Logger, net libp2pnetwork.Network, maxTrims int) {
	allPeers := net.Peers()
	before := len(allPeers)
	trimmed := make([]peer.ID, 0)
	for _, pid := range allPeers {
		if !c.connManager.IsProtected(pid, ProtectedTag) {
			if err := c.disconnect(pid, net); err != nil {
				logger.Debug("error closing peer", fields.PeerID(pid), zap.Error(err))
			}
			trimmed = append(trimmed, pid)
			TrimmedRecently.Set(pid, struct{}{}, ttlcache.DefaultTTL) // record stats
			if len(trimmed) >= maxTrims {
				break
			}
		}
	}
	logger.Debug("trimmed peers", zap.Int("peers_before_trim_total", before),
		zap.Int("peers_after_trim_total", len(net.Peers())), zap.Any("trimmed_peers", trimmed))
}

// DisconnectFromBadPeers will disconnect from bad peers according to their Gossip scores. It returns the number of disconnected peers.
func (c connManager) DisconnectFromBadPeers(logger *zap.Logger, net libp2pnetwork.Network, allPeers []peer.ID) int {
	disconnectedPeers := 0
	for _, peerID := range allPeers {
		// Disconnect if peer has bad gossip score.
		if isBad, gossipScore := c.gossipScoreIndex.HasBadGossipScore(peerID); isBad {
			err := c.disconnect(peerID, net)
			if err != nil {
				logger.Error("failed to disconnect from bad peer", fields.PeerID(peerID), zap.Float64("gossip_score", gossipScore))
			} else {
				logger.Debug("disconnecting from bad peer", fields.PeerID(peerID), zap.Float64("gossip_score", gossipScore))
				disconnectedPeers++
			}
		}
	}

	return disconnectedPeers
}

// DisconnectFromIrrelevantPeers will disconnect from at most [disconnectQuota] peers that doesn't share any subnet in common. It returns the number of disconnected peers.
func (c connManager) DisconnectFromIrrelevantPeers(logger *zap.Logger, disconnectQuota int, net libp2pnetwork.Network, allPeers []peer.ID, mySubnets records.Subnets) int {
	disconnectedPeers := 0
	for _, peerID := range allPeers {
		peerSubnets := c.subnetsIdx.GetPeerSubnets(peerID)
		sharedSubnets := records.SharedSubnets(mySubnets, peerSubnets, 0)

		// If there's no common subnet, disconnect from peer.
		if len(sharedSubnets) == 0 {
			err := c.disconnect(peerID, net)
			if err != nil {
				logger.Error("failed to disconnect from peer with irrelevant subnets", fields.PeerID(peerID))
			} else {
				logger.Debug("disconnecting from peer with irrelevant subnets", fields.PeerID(peerID))
				disconnectedPeers++
				if disconnectedPeers >= disconnectQuota {
					return disconnectedPeers
				}
			}
		}
	}
	return disconnectedPeers
}
