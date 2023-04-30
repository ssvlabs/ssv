package peers

import (
	"context"
	"math"
	"sort"

	"github.com/bloxapp/ssv/network/records"
	connmgrcore "github.com/libp2p/go-libp2p/core/connmgr"
	libp2pnetwork "github.com/libp2p/go-libp2p/core/network"
	"github.com/libp2p/go-libp2p/core/peer"
	"go.uber.org/zap"
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
}

// NewConnManager creates a new conn manager.
// multiple instances can be created, but concurrency is not supported.
func NewConnManager(logger *zap.Logger, connMgr connmgrcore.ConnManager, subnetsIdx SubnetsIndex) ConnManager {
	return &connManager{
		logger:      logger,
		connManager: connMgr,
		subnetsIdx:  subnetsIdx,
	}
}

// connManager implements ConnManager
type connManager struct {
	logger      *zap.Logger
	connManager connmgrcore.ConnManager
	subnetsIdx  SubnetsIndex
}

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

func (c connManager) TrimPeers(ctx context.Context, logger *zap.Logger, net libp2pnetwork.Network) {
	allPeers := net.Peers()
	before := len(allPeers)
	// TODO: use libp2p's conn manager once ready
	// c.connManager.TrimOpenConns(ctx)
	for _, pid := range allPeers {
		if !c.connManager.IsProtected(pid, protectedTag) {
			err := net.ClosePeer(pid)
			logger.Debug("DUMP: closing peer", zap.String("pid", pid.String()), zap.Error(err))
			// if err != nil {
			//	logger.Debug("could not close trimmed peer",
			//		zap.String("pid", pid.String()), zap.Error(err))
			//}
		}
	}
	logger.Debug("after trimming of peers", zap.Int("beforeTrim", before),
		zap.Int("afterTrim", len(net.Peers())))
}

// getBestPeers loop over all the existing peers and returns the best set
// according to the number of shared subnets,
// while considering subnets with low peer count to be more important.
func (c connManager) getBestPeers(n int, mySubnets records.Subnets, allPeers []peer.ID, topicMaxPeers int) map[peer.ID]PeerScore {
	peerScores := make(map[peer.ID]PeerScore)
	if len(allPeers) < n {
		for _, p := range allPeers {
			peerScores[p] = 1
		}
		return peerScores
	}
	stats := c.subnetsIdx.GetSubnetsStats()
	minSubnetPeers := 2
	subnetsScores := GetSubnetsDistributionScores(stats, minSubnetPeers, mySubnets, topicMaxPeers)

	type peerDump struct {
		Peer          peer.ID
		Score         PeerScore
		SharedSubnets int
		Subnets       string
	}
	var peerDumps []peerDump

	for _, pid := range allPeers {
		peerSubnets := c.subnetsIdx.GetPeerSubnets(pid)
		var score PeerScore
		if len(peerSubnets) == 0 {
			c.logger.Debug("DUMP: peer has no subnets", zap.String("peer", pid.String()))
			score = -2
		} else {
			score = scorePeer(peerSubnets, subnetsScores)
		}
		peerScores[pid] = score
		peerDumps = append(peerDumps, peerDump{
			Peer:          pid,
			Score:         score,
			SharedSubnets: len(records.SharedSubnets(peerSubnets, mySubnets, len(mySubnets))),
			Subnets:       peerSubnets.String(),
		})
	}

	sort.Slice(peerDumps, func(i, j int) bool {
		return peerDumps[i].Score < peerDumps[j].Score
	})

	var min, max = math.MaxInt32, math.MinInt32
	var sum, zeros, belowTwo int
	for _, n := range stats.Connected {
		if n > max {
			max = n
		}
		if n < min {
			min = n
		}
		if n == 0 {
			zeros++
		}
		if n < 2 {
			belowTwo++
		}
		sum += n
	}

	c.logger.Debug("DUMP: peer scores",
		zap.Int("total_subnets", len(mySubnets)),
		zap.Any("dump", map[string]interface{}{
			"my_subnets":          mySubnets.String(),
			"subnets_connections": stats.Connected,
			"peers":               peerDumps,
		}),
		zap.Any("subnet_stats", map[string]interface{}{
			"min":       min,
			"max":       max,
			"avg":       float64(sum) / float64(len(stats.Connected)),
			"zeros":     zeros,
			"below_two": belowTwo,
		}),
	)

	return GetTopScores(peerScores, n)
}

func scorePeer(peerSubnets records.Subnets, subnetsScores []float64) PeerScore {
	var score float64
	var connectedSubnets int
	for subnet, subnetScore := range subnetsScores {
		connected := peerSubnets[subnet] > 0
		if connected {
			score += subnetScore
			connectedSubnets++
		} else {
			// score -= subnetScore
		}
	}
	return PeerScore(score / float64(connectedSubnets))
}
