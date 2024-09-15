package peers

import (
	"context"
	"encoding/base64"
	"fmt"
	"math"
	"sort"

	connmgrcore "github.com/libp2p/go-libp2p/core/connmgr"
	libp2pnetwork "github.com/libp2p/go-libp2p/core/network"
	"github.com/libp2p/go-libp2p/core/peer"
	"go.uber.org/zap"

	"github.com/ssvlabs/ssv/logging/fields"
	"github.com/ssvlabs/ssv/network/records"
)

const (
	protectedTag = "ssv/subnets"
)

type PeerScore float64

// ConnManager is a wrapper on top of go-libp2p/core/connmgr.ConnManager.
// exposing an abstract interface so we can have the flexibility of doing some stuff manually
// rather than relaying on libp2p's connection manager.
type ConnManager interface {
	// TagBestPeers tags the best n peers from the given list, based on subnets distribution scores.
	TagBestPeers(logger *zap.Logger, n int, mySubnets records.Subnets, allPeers []peer.ID, topicMaxPeers int)
	// TrimPeers will trim unprotected peers.
	TrimPeers(ctx context.Context, logger *zap.Logger, net libp2pnetwork.Network)
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

// Set the "Protect" tag for the best [n] peers. For the others, set the "Unprotect" tag
func (c connManager) TagBestPeers(logger *zap.Logger, n int, mySubnets records.Subnets, allPeers []peer.ID, topicMaxPeers int) {
	bestPeers := c.getBestPeers(n, mySubnets, allPeers, topicMaxPeers)
	logger.Debug("tagging best peers",
		zap.Int("n", n),
		zap.Int("allPeers", len(allPeers)),
		zap.Int("bestPeers", len(bestPeers)))
	if len(bestPeers) == 0 {
		return
	}
	for _, pid := range allPeers {
		if _, ok := bestPeers[pid]; ok {
			c.connManager.Protect(pid, protectedTag)
			continue
		}
		c.connManager.Unprotect(pid, protectedTag)
	}
}

// Closes the connection to all peers that are not protected
func (c connManager) TrimPeers(ctx context.Context, logger *zap.Logger, net libp2pnetwork.Network) {
	allPeers := net.Peers()
	before := len(allPeers)
	// TODO: use libp2p's conn manager once ready
	// c.connManager.TrimOpenConns(ctx)
	for _, pid := range allPeers {
		if !c.connManager.IsProtected(pid, protectedTag) {
			err := c.disconnect(pid, net)
			logger.Debug("closing peer", fields.PeerID(pid), zap.Error(err))
		}
	}
	logger.Debug("trimmed peers", zap.Int("beforeTrim", before),
		zap.Int("afterTrim", len(net.Peers())))
}

// getBestPeers loop over all the existing peers and returns the best set with [n] peers
// according to the number of shared subnets,
// while considering subnets with low peer count to be more important.
func (c connManager) getBestPeers(n int, mySubnets records.Subnets, allPeers []peer.ID, topicMaxPeers int) map[peer.ID]PeerScore {
	// If we have less than n peers, just return all as the best peers
	peerScores := make(map[peer.ID]PeerScore)
	if len(allPeers) < n {
		for _, p := range allPeers {
			peerScores[p] = 1
		}
		return peerScores
	}

	// Get score for each subnet
	stats := c.subnetsIdx.GetSubnetsStats()
	minSubnetPeers := 4
	subnetsScores := GetSubnetsDistributionScores(stats, minSubnetPeers, mySubnets, topicMaxPeers)

	// Compute the score for each peer according to peer's subnets and subnets' score
	var peerLogs []peerLog
	for _, pid := range allPeers {
		peerSubnets := c.subnetsIdx.GetPeerSubnets(pid)
		var score PeerScore
		if len(peerSubnets) == 0 {
			// TODO: shouldn't we not connect to peers with no subnets?
			c.logger.Debug("peer has no subnets", zap.String("peer", pid.String()))
			score = -1000
		} else {
			score = scorePeer(peerSubnets, subnetsScores)
		}
		peerScores[pid] = score
		peerLogs = append(peerLogs, peerLog{
			Peer:          pid,
			Score:         score,
			SharedSubnets: len(records.SharedSubnets(peerSubnets, mySubnets, len(mySubnets))),
		})
	}

	c.logPeerScores(peerLogs, mySubnets, stats.Connected)

	// Returns the [n] best peers
	return GetTopScores(peerScores, n)
}

type peerLog struct {
	Peer          peer.ID
	Score         PeerScore
	SharedSubnets int
}

func (c connManager) logPeerScores(peerLogs []peerLog, mySubnets records.Subnets, subnetConnections []int) {
	sort.Slice(peerLogs, func(i, j int) bool {
		return peerLogs[i].Score < peerLogs[j].Score
	})

	// Calculate min & max of the active subnet connections.
	activeSubnetConnections := make([]int, 0, mySubnets.Active())
	var min, max = math.MaxInt32, math.MinInt32
	for subnet, n := range subnetConnections {
		if mySubnets[subnet] <= 0 {
			continue
		}
		activeSubnetConnections = append(activeSubnetConnections, n)
		if n > max {
			max = n
		}
		if n < min {
			min = n
		}
	}

	// Calculate the median of the active subnet connections.
	median := 0.0
	if len(activeSubnetConnections) > 0 {
		sort.Ints(activeSubnetConnections)
		if len(activeSubnetConnections)%2 == 0 {
			median = float64(activeSubnetConnections[len(activeSubnetConnections)/2-1]+activeSubnetConnections[len(activeSubnetConnections)/2]) / 2.0
		} else {
			median = float64(activeSubnetConnections[len(activeSubnetConnections)/2])
		}
	}

	// Encode the subnet connections as a base64 string.
	b := make([]byte, len(subnetConnections))
	for subnet, n := range subnetConnections {
		b[subnet] = byte(n)
	}
	subnetConnectionsBase64 := base64.StdEncoding.EncodeToString(b)

	c.logger.Debug("scored peers",
		zap.Any("subnet_stats", map[string]string{
			"min":    fmt.Sprintf("%d", min),
			"max":    fmt.Sprintf("%d", max),
			"median": fmt.Sprintf("%.1f", median),
		}),
		zap.String("subnet_connections", subnetConnectionsBase64),
		zap.Any("peers", peerLogs),
	)
}

func scorePeer(peerSubnets records.Subnets, subnetsScores []float64) PeerScore {
	var score float64
	for subnet, subnetScore := range subnetsScores {
		connected := peerSubnets[subnet] > 0
		if connected {
			score += subnetScore
		} else {
			score -= subnetScore
		}
	}
	return PeerScore(score)
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
		sharedSubnets := records.SharedSubnets(mySubnets, peerSubnets, len(mySubnets))

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
