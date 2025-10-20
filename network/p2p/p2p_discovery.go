package p2pv1

import (
	"fmt"
	"math"
	"strconv"
	"time"

	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/oleiade/lane/v2"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
	"go.uber.org/zap"

	"github.com/ssvlabs/ssv/network/commons"
	"github.com/ssvlabs/ssv/network/discovery"
	"github.com/ssvlabs/ssv/observability"
	"github.com/ssvlabs/ssv/observability/log/fields"
	"github.com/ssvlabs/ssv/utils/async"
)

func (n *p2pNetwork) startDiscovery() error {
	startTime := time.Now()
	_, span := tracer.Start(n.ctx, observability.InstrumentName(observabilityNamespace, "discovery.start"))
	defer span.End()

	connector, err := n.getConnector()
	if err != nil {
		span.SetStatus(codes.Error, err.Error())
		return err
	}

	// Spawn a goroutine to deduplicate discovered peers by peer ID.
	connectorProposals := make(chan peer.AddrInfo, connectorQueueSize)
	go n.bootstrapDiscovery(connectorProposals)
	go func() {
		// Trace proposals arriving from discovery bootstrap and being recorded in the pool.
		_, gspan := tracer.Start(n.ctx, observability.InstrumentName(observabilityNamespace, "discovery.proposals"))
		defer gspan.End()
		for proposal := range connectorProposals {
			gspan.AddEvent("proposal_observed", trace.WithAttributes(
				attribute.String("ssv.p2p.peer.id", proposal.ID.String()),
			))
			discoveredPeer := discovery.DiscoveredPeer{
				AddrInfo: proposal,
				Tries:    0,
			}
			n.discoveredPeersPool.Set(proposal.ID, discoveredPeer)
		}
		gspan.SetStatus(codes.Ok, "")
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
		_, selSpan := tracer.Start(n.ctx, observability.InstrumentName(observabilityNamespace, "discovery.peer_selection"))
		defer selSpan.End()
		// Collect enough peers first to increase the quality of peer selection.
		const minDiscoveryTime = 1 * time.Minute
		if time.Since(startTime) < minDiscoveryTime {
			selSpan.AddEvent("skip_min_discovery_time", trace.WithAttributes(
				attribute.Int64("ssv.p2p.discovery.elapsed_sec", int64(time.Since(startTime).Seconds())),
			))
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
			selSpan.AddEvent("skip_no_vacant_slots", trace.WithAttributes(
				attribute.Int("inbound", inbound),
				attribute.Int("outbound", outbound),
				attribute.Int("max_peers", n.cfg.MaxPeers),
			))
			return
		}

		// Compute number of peers we're connected to for each subnet.
		ownSubnets := n.SubscribedSubnets()
		currentSubnetPeers := SubnetPeers{}
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
		selSpan.SetAttributes(
			attribute.Int("pool_size", n.discoveredPeersPool.SlowLen()),
			attribute.String("ssv.p2p.own_subnet_peers", currentSubnetPeers.String()),
		)

		// Limit new connections to the remaining outbound slots.
		maxPeersToConnect := max(vacantOutboundSlots, 1)

		// Repeatedly select the next best peer to connect to,
		// adding its subnets to pendingSubnetPeers so that the next selection
		// is scored assuming the previous peers are already connected.
		pendingSubnetPeers := SubnetPeers{}
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
				// - the more a peer has been tried the less relevant it is (cooldown grows)
				// - the more time has passed since last connect attempt the more relevant peer is (waited grows)
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

			bestPeer, _, ok := peersByPriority.Pop()
			if !ok {
				// No more peers.
				selSpan.AddEvent("no_candidates")
				break
			}

			// Add the selected peer's subnets to pendingSubnetPeers,
			// to be used in the next iteration.
			bestPeerSubnets := SubnetPeers{}
			subnets, _ := n.PeersIndex().GetPeerSubnets(bestPeer.ID)
			for subnet, v := range subnets.SubnetList() {
				bestPeerSubnets[subnet] = uint16(v) // #nosec G115 -- subnets has a constant max len of 128
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
			selSpan.AddEvent("best_peer_selected", trace.WithAttributes(
				attribute.String("ssv.p2p.peer.id", bestPeer.ID.String()),
				attribute.String("peer_subnets", bestPeerSubnets.String()),
				attribute.Int("sample_size", int(peersByPriority.Size())),
				attribute.Float64("min_score", minScore),
				attribute.Float64("max_score", maxScore),
				attribute.String("iteration", fmt.Sprintf("%d/%d", i, maxPeersToConnect)),
			))
		}

		// Forward the selected peers for connection, incrementing the retry counter.
		for _, p := range peersToConnect {
			n.discoveredPeersPool.Set(p.ID, discovery.DiscoveredPeer{
				AddrInfo: p.AddrInfo,
				Tries:    p.Tries + 1,
				LastTry:  time.Now(),
			})
			connector <- p.AddrInfo
			selSpan.AddEvent("peer_connect_proposed", trace.WithAttributes(
				attribute.String("ssv.p2p.peer.id", p.ID.String()),
			))
		}
		n.logger.Info(
			"proposed discovered peers",
			zap.Int("count", len(peersToConnect)),
		)
		selSpan.SetAttributes(attribute.Int("proposed_count", len(peersToConnect)))
		selSpan.SetStatus(codes.Ok, "")
	})

	span.SetStatus(codes.Ok, "")
	return nil
}
