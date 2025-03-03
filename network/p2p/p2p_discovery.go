package p2pv1

import (
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"

	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/oleiade/lane/v2"
	"github.com/ssvlabs/ssv/logging/fields"
	"github.com/ssvlabs/ssv/network/commons"
	"github.com/ssvlabs/ssv/network/discovery"
	"github.com/ssvlabs/ssv/utils/async"
	"go.uber.org/zap"
)

// SubnetPeers contains the number of peers we are connected to for each subnet.
type SubnetPeers [commons.SubnetsCount]uint16

func (a SubnetPeers) Add(b SubnetPeers) SubnetPeers {
	var sum SubnetPeers
	for i := range a {
		sum[i] = a[i] + b[i]
	}
	return sum
}

// Score estimates how much the given peer would contribute to our subscribed subnets.
// Score only rewards for shared subnets in which we don't have enough peers.
func (a SubnetPeers) Score(ours, theirs commons.Subnets) float64 {
	const (
		duoSubnetPriority  = 1
		soloSubnetPriority = 3
		deadSubnetPriority = 90
	)
	score := float64(0)
	for i := range a {
		if ours[i] > 0 && theirs[i] > 0 {
			switch a[i] {
			case 0:
				score += deadSubnetPriority
			case 1:
				score += soloSubnetPriority
			case 2:
				score += duoSubnetPriority
			}
		}
	}
	return score
}

func (a SubnetPeers) String() string {
	var b strings.Builder
	for i, v := range a {
		if v > 0 {
			fmt.Fprintf(&b, "%d:%d ", i, v)
		}
	}
	return b.String()
}

func (n *p2pNetwork) startDiscovery(logger *zap.Logger) error {
	startTime := time.Now()

	connector, err := n.getConnector()
	if err != nil {
		return err
	}

	// Spawn a goroutine to deduplicate discovered peers by peer ID.
	connectorProposals := make(chan peer.AddrInfo, connectorQueueSize)
	go n.bootstrapDiscovery(logger, connectorProposals)
	go func() {
		for proposal := range connectorProposals {
			discoveredPeer := discovery.DiscoveredPeer{
				AddrInfo: proposal,
				Tries:    0,
			}
			n.discoveredPeersPool.Set(proposal.ID, discoveredPeer)
		}
	}()

	// Spawn a goroutine to repeatedly select & connect to the best peers.
	// TODO: insert description of the mechanism here below
	const (
		retryCooldown      = 30 * time.Second
		retryCooldownLimit = 300 * time.Second
	)
	async.Interval(n.ctx, 15*time.Second, func() {
		// Collect enough peers first to increase the quality of peer selection.
		const minDiscoveryTime = 1 * time.Minute
		if time.Since(startTime) < minDiscoveryTime {
			return
		}

		// Avoid connecting to more peers if we're already at the limit.
		inbound, outbound := n.connectionStats()
		vacantOutboundSlots := n.cfg.MaxPeers - (inbound + outbound)
		if vacantOutboundSlots <= 0 {
			n.logger.Debug(
				"no vacant outbound slots, skipping peer selection",
				zap.Int("inbound_peers", inbound),
				zap.Int("outbound_peers", outbound),
				zap.Int("max_peers", n.cfg.MaxPeers),
			)
			return
		}

		// Compute number of peers we're connected to for each subnet.
		ownSubnets := n.SubscribedSubnets()
		ownSubnetPeers := SubnetPeers{}
		peersByTopic := n.PeersByTopic()
		for topic, peers := range peersByTopic {
			subnet, err := strconv.ParseInt(commons.GetTopicBaseName(topic), 10, 64)
			if err != nil {
				n.logger.Error("failed to parse topic",
					zap.String("topic", topic), zap.Error(err))
				continue
			}
			if subnet < 0 || subnet >= commons.SubnetsCount {
				n.logger.Error("invalid topic",
					zap.String("topic", topic), zap.Int("subnet", int(subnet)))
				continue
			}
			ownSubnetPeers[subnet] = uint16(len(peers))
		}

		n.logger.Debug("selecting discovered peers",
			zap.Int("pool_size", n.discoveredPeersPool.SlowLen()),
			zap.String("own_subnet_peers", ownSubnetPeers.String()))

		// Limit new connections to the remaining outbound slots.
		maxPeersToConnect := max(vacantOutboundSlots, 1)

		// Repeatedly select the next best peer to connect to,
		// adding its subnets to pendingSubnetPeers so that the next selection
		// is scored assuming the previous peers are already connected.
		pendingSubnetPeers := SubnetPeers{}
		peersToConnect := make(map[peer.ID]discovery.DiscoveredPeer)
		for i := range maxPeersToConnect {
			optimisticSubnetPeers := ownSubnetPeers.Add(pendingSubnetPeers)
			peersByPriority := lane.NewMaxPriorityQueue[discovery.DiscoveredPeer, float64]()
			minScore, maxScore := float64(math.MaxFloat64), float64(0)
			n.discoveredPeersPool.Range(func(peerID peer.ID, discoveredPeer discovery.DiscoveredPeer) bool {
				if _, ok := peersToConnect[peerID]; ok {
					// This peer was already selected.
					// n.logger.Debug(
					// 	"TODO: peer already selected, skipping",
					// 	fields.PeerID(peerID),
					// )
					return true
				}

				// Predict this peer's score by estimating how much it would contribute to our subscribed subnets.
				peerSubnets := n.PeersIndex().GetPeerSubnets(peerID)
				peerScore := optimisticSubnetPeers.Score(ownSubnets, peerSubnets)
				// n.logger.Debug(
				// 	"TODO: considering peer",
				// 	fields.PeerID(peerID),
				// 	zap.String("optimistic_subnet_peers", optimisticSubnetPeers.String()),
				// 	zap.String("peer_subnets", peerSubnets.String()),
				// 	zap.Uint64("peer_score", peerScore),
				// )

				// Apply backoff penalty for peers with failed connection attempts.
				if discoveredPeer.Tries > 0 {
					// Calculate the portion of the retry cooldown that has elapsed.
					cooldown := min(retryCooldownLimit, retryCooldown*time.Duration(discoveredPeer.Tries))
					waited := time.Since(discoveredPeer.LastTry)
					progress := min(1, float64(waited)/float64(cooldown))

					// Skip peers still in early cooldown.
					if progress < 0.1 {
						return true
					}

					// Reduce the peer score such that the more time has passed,
					// the lower the penalty becomes.
					peerScore *= progress
				}

				// Push the peer.
				peersByPriority.Push(discoveredPeer, peerScore)
				minScore = min(minScore, peerScore)
				maxScore = max(maxScore, peerScore)

				return true
			})

			bestPeer, _, ok := peersByPriority.Pop()
			if !ok {
				// No more peers.
				break
			}

			// Add the selected peer's subnets to pendingSubnetPeers,
			// to be used in the next iteration.
			bestPeerSubnets := SubnetPeers{}
			for subnet, v := range n.PeersIndex().GetPeerSubnets(bestPeer.ID) {
				bestPeerSubnets[subnet] = uint16(v)
			}
			pendingSubnetPeers = pendingSubnetPeers.Add(bestPeerSubnets)
			peersToConnect[bestPeer.ID] = bestPeer

			n.logger.Debug(
				"found the best peer to connect to",
				fields.PeerID(bestPeer.ID),
				zap.String("peer_subnets", bestPeerSubnets.String()),
				zap.Uint("sample_size", peersByPriority.Size()),
				zap.Float64("min_score", minScore),
				zap.Float64("max_score", maxScore),
				zap.String("iteration", fmt.Sprintf("%d of %d", i, maxPeersToConnect)),
			)
		}

		// Forward the selected peers for connection, incrementing the retry counter.
		for _, p := range peersToConnect {
			n.discoveredPeersPool.Set(p.ID, discovery.DiscoveredPeer{
				AddrInfo: p.AddrInfo,
				Tries:    p.Tries + 1,
				LastTry:  time.Now(),
			})
			connector <- p.AddrInfo
		}
		n.logger.Info(
			"proposed discovered peers",
			zap.Int("count", len(peersToConnect)),
		)
	})

	return nil
}
