package p2pv1

import (
	"fmt"
	"math"
	"strconv"
	"time"

	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/oleiade/lane/v2"
	"github.com/ssvlabs/ssv/network/commons"
	"github.com/ssvlabs/ssv/network/discovery"
	"github.com/ssvlabs/ssv/utils/async"
	"go.uber.org/zap"
)

// SubnetPeers contains the number of peers we are connected to for each subnet.
type SubnetPeers [commons.SubnetsCount]uint16

func (a SubnetPeers) Add(b SubnetPeers) SubnetPeers {
	result := SubnetPeers{}
	for i := 0; i < commons.SubnetsCount; i++ {
		result[i] = a[i] + b[i]
	}
	return result
}

// Score returns the score of the proposed peer based on the current subnets we are connected to.
func (a SubnetPeers) Score(b SubnetPeers) uint64 {
	const (
		duoSubnetPriority  = 1
		soloSubnetPriority = 3
		deadSubnetPriority = 90
	)
	score := uint64(0)
	for i := range commons.SubnetsCount {
		if b[i] > 0 {
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
		// keep discovered peers in a pool so we can choose the best ones
		for proposal := range connectorProposals {
			discoveredPeer := discovery.DiscoveredPeer{
				AddrInfo:       proposal,
				ConnectRetries: 0,
			}
			n.discoveredPeersPool.Set(proposal.ID, discoveredPeer)

			n.interfaceLogger.Debug(
				"discovery proposed peer, adding it to the pool",
				zap.String("peer_id", string(proposal.ID)),
			)
		}
	}()

	// Spawn a goroutine to repeatedly select & connect to the best peers.
	// TODO: insert description of the mechanism here below
	async.Interval(n.ctx, 15*time.Second, func() {
		// Collect enough peers first to increase the quality of peer selection.
		const minDiscoveryTime = 1 * time.Minute
		if time.Since(startTime) < minDiscoveryTime {
			n.discoveredPeersPool.SlowLen()
			return
		}

		n.interfaceLogger.Debug("starting selecting peers", zap.Int("pool_size", n.discoveredPeersPool.SlowLen()))

		// Avoid connecting to more peers if we're already at the limit.
		inbound, outbound := n.connectionStats()
		vacantOutboundSlots := n.cfg.MaxPeers - (inbound + outbound)
		if vacantOutboundSlots <= 0 {
			n.interfaceLogger.Debug(
				"no vacant outbound slots, skipping peer selection",
				zap.Int("inbound_peers", inbound),
				zap.Int("outbound_peers", outbound),
				zap.Int("max_peers", n.cfg.MaxPeers),
			)
			return
		}

		// Compute number of peers we're connected to for each subnet.
		ownSubnetPeers := SubnetPeers{}
		peersByTopic := n.PeersByTopic()
		for topic, peers := range peersByTopic {
			subnet, err := strconv.ParseInt(commons.GetTopicBaseName(topic), 10, 64)
			if err != nil {
				n.interfaceLogger.Error("cant parse topic",
					zap.String("topic", topic), zap.Error(err))
				continue
			}
			if subnet < 0 || subnet >= commons.SubnetsCount {
				n.interfaceLogger.Error("invalid topic",
					zap.String("topic", topic), zap.Int("subnet", int(subnet)))
				continue
			}
			ownSubnetPeers[subnet] = uint16(len(peers))
		}

		// Limit new connections to the remaining outbound slots.
		maxPeersToConnect := max(vacantOutboundSlots, 1)

		// Repeatedly select the next best peer to connect to,
		// adding its subnets to pendingSubnetPeers so that the next selection
		// is scored assuming the previous peers are already connected.
		pendingSubnetPeers := SubnetPeers{}
		peersToConnect := make(map[peer.ID]discovery.DiscoveredPeer)
		for i := range maxPeersToConnect {
			optimisticSubnetPeers := ownSubnetPeers.Add(pendingSubnetPeers)
			peersByPriority := lane.NewMaxPriorityQueue[discovery.DiscoveredPeer, uint64]()
			minScore, maxScore := uint64(math.MaxUint64), uint64(0)
			n.discoveredPeersPool.Range(func(peerID peer.ID, discoveredPeer discovery.DiscoveredPeer) bool {
				const retryLimit = 3
				if discoveredPeer.ConnectRetries >= retryLimit {
					// We've exhausted retry attempts for this peer.
					return true
				}

				if _, ok := peersToConnect[peerID]; ok {
					// This peer was already selected.
					return true
				}

				// Predict this peer's score by adding its subnets to pendingSubnetPeers
				// and then scoring the total.
				discPeerSubnetPeer := SubnetPeers{}
				for subnet, v := range n.PeersIndex().GetPeerSubnets(peerID) {
					discPeerSubnetPeer[subnet] = uint16(v)
				}
				peerScore := optimisticSubnetPeers.Score(discPeerSubnetPeer)
				peersByPriority.Push(discoveredPeer, peerScore)

				if minScore > peerScore {
					minScore = peerScore
				}
				if maxScore < peerScore {
					maxScore = peerScore
				}

				return true
			})

			bestPeer, _, ok := peersByPriority.Pop()
			if !ok {
				// No more peers.
				break
			}

			// Add the selected peer's subnets to pendingSubnetPeers,
			// to be used in the next iteration.
			bestPeerSubnetPeer := SubnetPeers{}
			for subnet, v := range n.PeersIndex().GetPeerSubnets(bestPeer.ID) {
				bestPeerSubnetPeer[subnet] = uint16(v)
			}
			pendingSubnetPeers = pendingSubnetPeers.Add(bestPeerSubnetPeer)
			peersToConnect[bestPeer.ID] = bestPeer

			n.interfaceLogger.Debug(
				"found the best peer to connect to",
				zap.Uint("sample_size", peersByPriority.Size()),
				zap.Uint64("min_score", minScore),
				zap.Uint64("max_score", maxScore),
				zap.String("iteration", fmt.Sprintf("%d of %d", i, maxPeersToConnect)),
			)
		}

		// Forward the selected peers for connection, incrementing the retry counter.
		for _, p := range peersToConnect {
			n.discoveredPeersPool.Set(p.ID, discovery.DiscoveredPeer{
				AddrInfo:       p.AddrInfo,
				ConnectRetries: p.ConnectRetries + 1,
			})
			connector <- p.AddrInfo
		}
		n.interfaceLogger.Info(
			"proposed discovered peers to try connect to",
			zap.Int("count", len(peersToConnect)),
		)
	})

	return nil
}
