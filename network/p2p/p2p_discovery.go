package p2pv1

import (
	"fmt"
	"math"
	"strconv"
	"time"

	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/oleiade/lane/v2"
	"go.uber.org/zap"

	"github.com/ssvlabs/ssv/network/commons"
	"github.com/ssvlabs/ssv/network/discovery"
	"github.com/ssvlabs/ssv/observability/log/fields"
	"github.com/ssvlabs/ssv/utils/async"
)

func (n *p2pNetwork) startDiscovery() error {
	startTime := time.Now()

	connector, err := n.getConnector()
	if err != nil {
		return err
	}

	// Spawn a goroutine to deduplicate discovered peers by peer ID.
	connectorProposals := make(chan peer.AddrInfo, connectorQueueSize)
	go n.bootstrapDiscovery(connectorProposals)
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
	// To find the best set of peers to connect we'll:
	// - iterate over all available candidate-peers (peers discovered so far) and choose the best one
	//   scoring peers based on how many dead/solo/duo subnets they resolve for us
	// - add the best peer to "peersToConnect" set assuming (optimistically) we are gonna successfully
	//   connect with this peer
	// - repeat those steps from above N times (depending on how many connection slots we have available),
	//   also taking into account "peersToConnect" set of peers on each consecutive iteration
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
			n.logger.Debug("no vacant outbound slots, skipping peer selection",
				zap.Int("inbound_peers", inbound),
				zap.Int("outbound_peers", outbound),
				zap.Int("max_peers", n.cfg.MaxPeers),
			)
			return
		}

		// Compute the number of peers we're connected to for each subnet.
		ownSubnets := n.SubscribedSubnets()
		currentSubnetPeers := newSubnetPeers()
		for topic, peers := range n.PeersByTopic() {
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
			currentSubnetPeers[subnet] = uint16(len(peers)) //nolint: gosec
		}

		n.logger.Debug("selecting discovered peers",
			zap.Int("pool_size", n.discoveredPeersPool.SlowLen()),
			zap.String("own_subnet_peers", currentSubnetPeers.String()))

		// Limit new connections to the remaining outbound slots.
		maxPeersToConnect := max(vacantOutboundSlots, 1)

		// Repeatedly select the next best peer to connect to, adding its subnets to pendingSubnetPeers
		// so that the next selection is scored assuming the previous peers are already connected.
		pendingSubnetPeers := newSubnetPeers()
		peersToConnect := make(map[peer.ID]discovery.DiscoveredPeer)
		for i := range maxPeersToConnect {
			optimisticSubnetPeers := currentSubnetPeers.Add(pendingSubnetPeers)
			peersByPriority := lane.NewMaxPriorityQueue[discovery.DiscoveredPeer, float64]()
			minScore, maxScore := math.MaxFloat64, float64(0)
			n.discoveredPeersPool.Range(func(peerID peer.ID, discoveredPeer discovery.DiscoveredPeer) bool {
				if _, ok := peersToConnect[peerID]; ok {
					// This peer was already selected.
					return true
				}

				// Predict this peer's score by estimating how much it would contribute to our subscribed subnets,
				// applying backoff penalty for peers with failed connection attempts:
				// - the more a peer has been tried, the less relevant it is (cooldown grows)
				// - the more time has passed since the last connect attempt the more relevant peer is (waited grows)
				peerSubnets, _ := n.PeersIndex().GetPeerSubnets(peerID)
				peerScore := optimisticSubnetPeers.Score(ownSubnets, peerSubnets)
				if discoveredPeer.Tries > 0 {
					const retryCooldownMin, retryCooldownMax = 30 * time.Second, 300 * time.Second
					waited := time.Since(discoveredPeer.LastTry)
					if waited < retryCooldownMin {
						return true // skip this peer to wait out at least minimal cooldown
					}
					cooldown := min(retryCooldownMax, retryCooldownMin*time.Duration(discoveredPeer.Tries))
					peerRelevance := min(1, float64(waited)/float64(cooldown))
					peerScore *= peerRelevance * peerRelevance
				}

				peersByPriority.Push(discoveredPeer, peerScore)
				minScore = min(minScore, peerScore)
				maxScore = max(maxScore, peerScore)

				return true
			})

			bestPeer, peerScore, ok := peersByPriority.Pop()
			if !ok {
				// No more peers.
				break
			}

			// Add the selected(best) peer's subnets to pendingSubnetPeers to be used on the next iteration.
			bestPeerSubnets, _ := n.PeersIndex().GetPeerSubnets(bestPeer.ID)
			bestSubnetPeers := newSubnetPeersFromSubnets(bestPeerSubnets)
			pendingSubnetPeers = pendingSubnetPeers.Add(bestSubnetPeers)
			peersToConnect[bestPeer.ID] = bestPeer

			n.logger.Debug("found the best peer to connect to",
				fields.PeerID(bestPeer.ID),
				zap.String("peer_subnets", bestPeerSubnets.StringHumanReadable()),
				zap.Float64("peer_score", peerScore),
				zap.Float64("min_score", minScore),
				zap.Float64("max_score", maxScore),
				zap.Uint("sample_size", peersByPriority.Size()+1),
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
		n.logger.Info("proposed discovered peers",
			zap.Int("count", len(peersToConnect)),
		)
	})

	return nil
}
