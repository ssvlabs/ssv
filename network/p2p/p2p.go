package p2pv1

import (
	"context"
	"errors"
	"fmt"
	"math"
	"math/rand"
	"slices"
	"strings"
	"sync/atomic"
	"time"

	"github.com/jellydator/ttlcache/v3"
	"github.com/libp2p/go-libp2p/core/connmgr"
	connmgrcore "github.com/libp2p/go-libp2p/core/connmgr"
	"github.com/libp2p/go-libp2p/core/host"
	"github.com/libp2p/go-libp2p/core/peer"
	libp2pdiscbackoff "github.com/libp2p/go-libp2p/p2p/discovery/backoff"
	ma "github.com/multiformats/go-multiaddr"
	"github.com/ssvlabs/ssv/logging"
	"github.com/ssvlabs/ssv/logging/fields"
	"github.com/ssvlabs/ssv/message/validation"
	"github.com/ssvlabs/ssv/network"
	"github.com/ssvlabs/ssv/network/commons"
	"github.com/ssvlabs/ssv/network/discovery"
	"github.com/ssvlabs/ssv/network/peers"
	"github.com/ssvlabs/ssv/network/peers/connections"
	"github.com/ssvlabs/ssv/network/records"
	"github.com/ssvlabs/ssv/network/streams"
	"github.com/ssvlabs/ssv/network/topics"
	operatordatastore "github.com/ssvlabs/ssv/operator/datastore"
	"github.com/ssvlabs/ssv/operator/keys"
	operatorstorage "github.com/ssvlabs/ssv/operator/storage"
	"github.com/ssvlabs/ssv/utils/async"
	"github.com/ssvlabs/ssv/utils/hashmap"
	"github.com/ssvlabs/ssv/utils/tasks"
	"go.uber.org/zap"
	"golang.org/x/exp/maps"
)

// network states
const (
	stateInitializing int32 = 0
	stateClosing      int32 = 1
	stateClosed       int32 = 2
	stateReady        int32 = 10
)

const (
	// peersTrimmingInterval defines how often we want to try and trim connected peers. This value
	// should be low enough for our node to find good set of peers reasonably fast (10-20 minutes)
	// after node start, but it shouldn't be too low since that might negatively affect Ethereum
	// duty execution quality.
	peersTrimmingInterval           = 60 * time.Second
	peersReportingInterval          = 60 * time.Second
	peerIdentitiesReportingInterval = 5 * time.Minute
	topicsReportingInterval         = 180 * time.Second
)

// PeersIndexProvider holds peers index instance
type PeersIndexProvider interface {
	PeersIndex() peers.Index
}

// HostProvider holds host instance
type HostProvider interface {
	Host() host.Host
}

// p2pNetwork implements network.P2PNetwork
type p2pNetwork struct {
	parentCtx context.Context
	ctx       context.Context
	cancel    context.CancelFunc

	interfaceLogger *zap.Logger // struct logger to log in interface methods that do not accept a logger
	cfg             *Config

	host         host.Host
	streamCtrl   streams.StreamController
	idx          peers.Index
	disc         discovery.Service
	topicsCtrl   topics.Controller
	msgRouter    network.MessageRouter
	msgResolver  topics.MsgPeersResolver
	msgValidator validation.MessageValidator
	connHandler  connections.ConnHandler
	connGater    connmgr.ConnectionGater
	trustedPeers []*peer.AddrInfo

	state int32

	activeCommittees *hashmap.Map[string, validatorStatus]

	backoffConnector *libp2pdiscbackoff.BackoffConnector

	fixedSubnets  []byte
	activeSubnets []byte

	libConnManager connmgrcore.ConnManager

	nodeStorage             operatorstorage.Storage
	operatorPKHashToPKCache *hashmap.Map[string, []byte] // used for metrics
	operatorSigner          keys.OperatorSigner
	operatorDataStore       operatordatastore.OperatorDataStore
}

// New creates a new p2p network
func New(logger *zap.Logger, cfg *Config) (*p2pNetwork, error) {
	ctx, cancel := context.WithCancel(cfg.Ctx)

	logger = logger.Named(logging.NameP2PNetwork)

	n := &p2pNetwork{
		parentCtx:               cfg.Ctx,
		ctx:                     ctx,
		cancel:                  cancel,
		interfaceLogger:         logger,
		cfg:                     cfg,
		msgRouter:               cfg.Router,
		msgValidator:            cfg.MessageValidator,
		state:                   stateClosed,
		activeCommittees:        hashmap.New[string, validatorStatus](),
		nodeStorage:             cfg.NodeStorage,
		operatorPKHashToPKCache: hashmap.New[string, []byte](),
		operatorSigner:          cfg.OperatorSigner,
		operatorDataStore:       cfg.OperatorDataStore,
	}
	if err := n.parseTrustedPeers(); err != nil {
		return nil, err
	}
	return n, nil
}

func (n *p2pNetwork) parseTrustedPeers() error {
	if len(n.cfg.TrustedPeers) == 0 {
		return nil // No trusted peers to parse, return early
	}
	// Group addresses by peer ID.
	trustedPeers := map[peer.ID][]ma.Multiaddr{}
	for _, mas := range n.cfg.TrustedPeers {
		for _, ma := range strings.Split(mas, ",") {
			addrInfo, err := peer.AddrInfoFromString(ma)
			if err != nil {
				return fmt.Errorf("could not parse trusted peer: %w", err)
			}
			trustedPeers[addrInfo.ID] = append(trustedPeers[addrInfo.ID], addrInfo.Addrs...)
		}
	}
	for id, addrs := range trustedPeers {
		n.trustedPeers = append(n.trustedPeers, &peer.AddrInfo{ID: id, Addrs: addrs})
	}
	return nil
}

// Host implements HostProvider
func (n *p2pNetwork) Host() host.Host {
	return n.host
}

// PeersIndex returns the peers index
func (n *p2pNetwork) PeersIndex() peers.Index {
	return n.idx
}

func (n *p2pNetwork) PeersByTopic() ([]peer.ID, map[string][]peer.ID) {
	var err error
	tpcs := n.topicsCtrl.Topics()
	peerz := make(map[string][]peer.ID, len(tpcs))
	for _, tpc := range tpcs {
		peerz[tpc], err = n.topicsCtrl.Peers(tpc)
		if err != nil {
			n.interfaceLogger.Error("Cant get peers from topics")
			return nil, nil
		}
	}
	allpeers, err := n.topicsCtrl.Peers("")
	if err != nil {
		n.interfaceLogger.Error("Cant all peers")
		return nil, nil
	}
	return allpeers, peerz
}

// Close implements io.Closer
func (n *p2pNetwork) Close() error {
	atomic.SwapInt32(&n.state, stateClosing)
	defer atomic.StoreInt32(&n.state, stateClosed)
	n.cancel()
	if err := n.libConnManager.Close(); err != nil {
		n.interfaceLogger.Warn("could not close discovery", zap.Error(err))
	}
	if err := n.disc.Close(); err != nil {
		n.interfaceLogger.Warn("could not close discovery", zap.Error(err))
	}
	if err := n.idx.Close(); err != nil {
		n.interfaceLogger.Warn("could not close index", zap.Error(err))
	}
	if err := n.topicsCtrl.Close(); err != nil {
		n.interfaceLogger.Warn("could not close topics controller", zap.Error(err))
	}
	return n.host.Close()
}

func (n *p2pNetwork) getConnector() (chan peer.AddrInfo, error) {
	connector := make(chan peer.AddrInfo, connectorQueueSize)
	go func() {
		// Wait for own subnets to be subscribed to and updated.
		// TODO: wait more intelligently with a channel.
		time.Sleep(8 * time.Second)
		ctx, cancel := context.WithCancel(n.ctx)
		defer cancel()
		n.backoffConnector.Connect(ctx, connector)
	}()

	// Connect to trusted peers first.
	go func() {
		for _, addrInfo := range n.trustedPeers {
			connector <- *addrInfo
		}
	}()

	return connector, nil
}

// Start starts the discovery service, garbage collector (peer index), and reporting.
func (n *p2pNetwork) Start(logger *zap.Logger) error {
	logger = logger.Named(logging.NameP2PNetwork)

	if atomic.SwapInt32(&n.state, stateReady) == stateReady {
		// return errors.New("could not setup network: in ready state")
		return nil
	}

	connector, err := n.getConnector()
	if err != nil {
		return err
	}

	pAddrs, err := peer.AddrInfoToP2pAddrs(&peer.AddrInfo{
		ID:    n.host.ID(),
		Addrs: n.host.Addrs(),
	})
	if err != nil {
		logger.Fatal("could not get my address", zap.Error(err))
	}
	maStrs := make([]string, len(pAddrs))
	for i, ima := range pAddrs {
		maStrs[i] = ima.String()
	}
	logger.Info("starting p2p",
		zap.String("my_address", strings.Join(maStrs, ",")),
		zap.Int("trusted_peers", len(n.trustedPeers)),
	)

	connectorProposals := make(chan peer.AddrInfo)
	go n.startDiscovery(logger, connectorProposals)
	go func() {
		// keep discovered peers in the pool so we can choose the best ones
		for proposal := range connectorProposals {
			if peers.DiscoveredPeersPool.Has(proposal.ID) {
				// TODO
				n.interfaceLogger.Info(
					"this proposal is already on the table",
					zap.String("peer_id", string(proposal.ID)),
				)
				continue // this proposal is already "on the table"
			}
			discoveredPeer := peers.DiscoveredPeer{
				AddrInfo:       proposal,
				ConnectRetries: 0,
			}
			peers.DiscoveredPeersPool.Set(proposal.ID, discoveredPeer, ttlcache.DefaultTTL)

			// TODO
			n.interfaceLogger.Info(
				"discovered new peer",
				zap.String("peer_id", string(proposal.ID)),
			)
		}
	}()
	// choose the best peer(s) from the pool of discovered peers to propose connecting to it
	async.Interval(n.ctx, 10*time.Second, func() {
		// find and propose the best discovered peer we can
		var (
			bestProposal      peers.DiscoveredPeer
			bestProposalFound bool
			bestProposalScore float64 // how good the proposed peer is
		)
		peers.DiscoveredPeersPool.Range(func(item *ttlcache.Item[peer.ID, peers.DiscoveredPeer]) bool {
			// TODO
			const retryLimit = 2
			//const retryLimit = 5
			if item.Value().ConnectRetries >= retryLimit {
				// this discovered peer has been tried many times already, we'll ignore him but won't
				// remove him from DiscoveredPeersPool since if we do - discovery might suggest this
				// peer again (essentially resetting this peer's retry attempts counter to 0)

				// TODO
				n.interfaceLogger.Info(
					"Not gonna propose discovered peer: ran out of retries",
					zap.String("peer_id", string(item.Key())),
				)

				return true
			}
			proposalScore := n.peerScore(item.Key())
			if proposalScore > bestProposalScore {
				bestProposal = item.Value()
				bestProposalFound = true
				bestProposalScore = proposalScore
			}
			return true
		})
		if !bestProposalFound {
			// TODO
			n.interfaceLogger.Error("No peer to propose this time around")
			return // we have not a single peer to propose
		}
		// update retry counter for this peer so we eventually skip it after certain number of retries
		// TODO ^ becase we aren't using mutex to make operations related to DiscoveredPeersPool atomic
		// it's better to do this before we send this proposal on `connector` to minimize the chance of
		// hitting the undesirable race condition (where we'll successfully connect to this peer but will
		// keep retrying until last allowed retry attempt)
		peers.DiscoveredPeersPool.Set(bestProposal.ID, peers.DiscoveredPeer{
			AddrInfo:       bestProposal.AddrInfo,
			ConnectRetries: bestProposal.ConnectRetries + 1,
		}, ttlcache.DefaultTTL)
		connector <- bestProposal.AddrInfo // try to connect to best peer

		// TODO
		n.interfaceLogger.Info(
			"Proposed discovered peer",
			zap.String("peer_id", string(bestProposal.ID)),
		)
	})

	async.Interval(n.ctx, peersTrimmingInterval, n.peersTrimming(logger))

	async.Interval(n.ctx, peersReportingInterval, recordPeerCount(n.ctx, logger, n.host))

	async.Interval(n.ctx, peerIdentitiesReportingInterval, recordPeerIdentities(n.ctx, n.host, n.idx))

	async.Interval(n.ctx, topicsReportingInterval, recordPeerCountPerTopic(n.ctx, logger, n.topicsCtrl, 2))

	if err := n.subscribeToFixedSubnets(logger); err != nil {
		return err
	}

	return nil
}

// Returns a function that trims currently connected peers if necessary, namely:
//   - Dropping peers with bad Gossip score.
//   - Dropping irrelevant peers that don't have any subnet in common.
//   - Tagging the best MaxPeers-N peers (according to subnets intersection) as Protected,
//     and then removing the worst peers. But only if we are close to MaxPeers limit.
func (n *p2pNetwork) peersTrimming(logger *zap.Logger) func() {
	return func() {
		ctx, cancel := context.WithTimeout(n.ctx, 60*time.Second)
		defer cancel()
		defer func() {
			_ = n.idx.GetSubnetsStats() // collect metrics
		}()

		connMgr := peers.NewConnManager(logger, n.libConnManager, n.idx, n.idx)

		disconnectedCnt := connMgr.DisconnectFromBadPeers(logger, n.host.Network(), n.host.Network().Peers())
		if disconnectedCnt > 0 {
			// we can accept more peer connections now, no need to trim
			return
		}

		connectedPeers := n.host.Network().Peers()

		const maximumIrrelevantPeersToDisconnect = 3
		disconnectedCnt = connMgr.DisconnectFromIrrelevantPeers(
			logger,
			maximumIrrelevantPeersToDisconnect,
			n.host.Network(),
			connectedPeers,
			n.activeSubnets,
		)
		if disconnectedCnt > 0 {
			// we can accept more peer connections now, no need to trim
			return
		}

		// maxPeersToDrop value should be in the range of 3-5% of MaxPeers for trimming to work
		// fast enough so that our node finds good set of peers within 10-20 minutes after node
		// start; it shouldn't be too large because that would negatively affect Ethereum duty
		// execution quality
		//const maxPeersToDrop = 4 // targeting MaxPeers in 60-90 range
		// TODO
		const maxPeersToDrop = 1

		// see if we can accept more peer connections already (no need to trim), note we trim not
		// only when our current connections reach MaxPeers limit exactly but even if we get close
		// enough to it - this ensures we don't skip trim iteration because of "random fluctuations"
		// in currently connected peer count at that limit boundary
		connectedPeers = n.host.Network().Peers()
		if len(connectedPeers) <= n.cfg.MaxPeers-maxPeersToDrop {
			// we probably don't want to trim then

			// additionally, make sure incoming connections aren't at the limit - since if they are we
			// actually want to try and trim some of them to make sure we re-cycle incoming connections
			// at least occasionally (note btw, with current implementation there is no guarantee incoming
			// connections will be trimmed in this case, since we don't differentiate between incoming/outgoing
			// when trimming)
			in, _ := n.connectionStats()
			inboundLimit := int(float64(n.cfg.MaxPeers) * inboundLimitRatio)
			if in < inboundLimit {
				return // skip trim iteration
			}
			// we don't want to trim incoming connections as often as outgoing connections (since trimming
			// outgoing connections often helps us discover valuable peers, while it's not really the case
			// for with incoming connections - only slightly so), hence we'll only do it 1/5 of the times
			if rand.Intn(5) > 0 { // nolint: gosec
				return // skip trim iteration
			}
		}

		// gotta trim some peers then
		immunityQuota := len(connectedPeers) - maxPeersToDrop
		protectedPeers := n.PeerProtection(immunityQuota)
		for _, peer := range connectedPeers {
			if _, ok := protectedPeers[peer]; ok {
				n.libConnManager.Protect(peer, peers.ProtectedTag)
				continue
			}
			n.libConnManager.Unprotect(peer, peers.ProtectedTag)
		}
		connMgr.TrimPeers(ctx, logger, n.host.Network(), maxPeersToDrop) // trim up to maxPeersToDrop
	}
}

// PeerProtection returns a map of protected peers based on the intersection of subnets.
// it protects the best peers by these rules:
// - At least 2 peers per subnet.
// - Prefer peers that you have more shared subents with.
// - Protect at most immunityQuota peers.
func (n *p2pNetwork) PeerProtection(immunityQuota int) map[peer.ID]struct{} {
	myPeersSet := make(map[peer.ID]struct{}, 0)
	for _, tpc := range n.topicsCtrl.Topics() {
		peerz, err := n.topicsCtrl.Peers(tpc)
		if err != nil {
			n.interfaceLogger.Error(
				"Cant get peers for topic, skipping to keep the network running",
				zap.String("topic", tpc),
				zap.Error(err),
			)
			continue
		}
		for _, p := range peerz {
			myPeersSet[p] = struct{}{}
		}
	}

	myPeers := maps.Keys(myPeersSet)
	slices.SortFunc(myPeers, func(a, b peer.ID) int {
		// sort in desc order (peers with the highest scores come first)
		if n.peerScore(a) < n.peerScore(b) {
			return 1
		}
		if n.peerScore(a) > n.peerScore(b) {
			return -1
		}
		return 0
	})

	protectedPeers := make(map[peer.ID]struct{})
	for i, p := range myPeers {
		if i+1 > immunityQuota {
			break
		}
		protectedPeers[p] = struct{}{}
	}
	return protectedPeers
}

// startDiscovery starts the required services
// it will try to bootstrap discovery service, and inject a connect function.
// the connect function checks if we can connect to the given peer and if so passing it to the backoff connector.
func (n *p2pNetwork) startDiscovery(logger *zap.Logger, connector chan peer.AddrInfo) {
	err := tasks.Retry(func() error {
		return n.disc.Bootstrap(logger, func(e discovery.PeerEvent) {
			if err := n.idx.CanConnect(e.AddrInfo.ID); err != nil {
				logger.Debug("skipping new peer", fields.PeerID(e.AddrInfo.ID), zap.Error(err))
				return
			}
			select {
			case connector <- e.AddrInfo:
			default:
				logger.Warn("connector queue is full, skipping new peer", fields.PeerID(e.AddrInfo.ID))
			}
		})
	}, 3)
	if err != nil {
		logger.Panic("could not setup discovery", zap.Error(err))
	}
}

func (n *p2pNetwork) isReady() bool {
	return atomic.LoadInt32(&n.state) == stateReady
}

// UpdateSubnets will update the registered subnets according to active validators
// NOTE: it won't subscribe to the subnets (use subscribeToFixedSubnets for that)
func (n *p2pNetwork) UpdateSubnets(logger *zap.Logger) {
	// TODO: this is a temporary fix to update subnets when validators are added/removed,
	// there is a pending PR to replace this: https://github.com/ssvlabs/ssv/pull/990
	logger = logger.Named(logging.NameP2PNetwork)
	ticker := time.NewTicker(time.Second)
	registeredSubnets := make([]byte, commons.Subnets())
	defer ticker.Stop()

	// Run immediately and then every second.
	for ; true; <-ticker.C {
		start := time.Now()

		updatedSubnets := n.SubscribedSubnets()
		n.activeSubnets = updatedSubnets

		// Compute the not yet registered subnets.
		addedSubnets := make([]uint64, 0)
		for subnet, active := range updatedSubnets {
			if active == byte(1) && registeredSubnets[subnet] == byte(0) {
				addedSubnets = append(addedSubnets, uint64(subnet)) // #nosec G115 -- subnets has a constant max len of 128
			}
		}

		// Compute the not anymore registered subnets.
		removedSubnets := make([]uint64, 0)
		for subnet, active := range registeredSubnets {
			if active == byte(1) && updatedSubnets[subnet] == byte(0) {
				removedSubnets = append(removedSubnets, uint64(subnet)) // #nosec G115 -- subnets has a constant max len of 128
			}
		}

		registeredSubnets = updatedSubnets

		if len(addedSubnets) == 0 && len(removedSubnets) == 0 {
			continue
		}

		n.idx.UpdateSelfRecord(func(self *records.NodeInfo) *records.NodeInfo {
			self.Metadata.Subnets = records.Subnets(n.activeSubnets).String()
			return self
		})

		// Register/unregister subnets for discovery.
		var errs error
		var hasAdded, hasRemoved bool
		if len(addedSubnets) > 0 {
			var err error
			hasAdded, err = n.disc.RegisterSubnets(logger.Named(logging.NameDiscoveryService), addedSubnets...)
			if err != nil {
				logger.Debug("could not register subnets", zap.Error(err))
				errs = errors.Join(errs, err)
			}
		}
		if len(removedSubnets) > 0 {
			var err error
			hasRemoved, err = n.disc.DeregisterSubnets(logger.Named(logging.NameDiscoveryService), removedSubnets...)
			if err != nil {
				logger.Debug("could not unregister subnets", zap.Error(err))
				errs = errors.Join(errs, err)
			}

			// Unsubscribe from the removed subnets.
			for _, removedSubnet := range removedSubnets {
				if err := n.unsubscribeSubnet(logger, removedSubnet); err != nil {
					logger.Debug("could not unsubscribe from subnet", zap.Uint64("subnet", removedSubnet), zap.Error(err))
					errs = errors.Join(errs, err)
				} else {
					logger.Debug("unsubscribed from subnet", zap.Uint64("subnet", removedSubnet))
				}
			}
		}
		if hasAdded || hasRemoved {
			go n.disc.PublishENR(logger.Named(logging.NameDiscoveryService))
		}

		allSubs, _ := records.Subnets{}.FromString(records.AllSubnets)
		subnetsList := records.SharedSubnets(allSubs, n.activeSubnets, 0)
		logger.Debug("updated subnets",
			zap.Any("added", addedSubnets),
			zap.Any("removed", removedSubnets),
			zap.Any("subnets", subnetsList),
			zap.Any("subscribed_topics", n.topicsCtrl.Topics()),
			zap.Int("total_subnets", len(subnetsList)),
			zap.Duration("took", time.Since(start)),
			zap.Error(errs),
		)
	}
}

// UpdateScoreParams updates the scoring parameters once per epoch through the call of n.topicsCtrl.UpdateScoreParams
func (n *p2pNetwork) UpdateScoreParams(logger *zap.Logger) {
	// TODO: this is a temporary solution to update the score parameters periodically.
	// But, we should use an appropriate trigger for the UpdateScoreParams function that should be
	// called once a validator is added or removed from the network

	logger = logger.Named(logging.NameP2PNetwork)

	// function to get the starting time of the next epoch
	nextEpochStartingTime := func() time.Time {
		currEpoch := n.cfg.Network.Beacon.EstimatedCurrentEpoch()
		nextEpoch := currEpoch + 1
		return n.cfg.Network.Beacon.EpochStartTime(nextEpoch)
	}

	// Create timer that triggers on the beginning of the next epoch
	timer := time.NewTimer(time.Until(nextEpochStartingTime()))
	defer timer.Stop()

	// Run immediately and then once every epoch
	for ; true; <-timer.C {

		// Update score parameters
		err := n.topicsCtrl.UpdateScoreParams(logger)
		if err != nil {
			logger.Debug("score parameters update failed", zap.Error(err))
		} else {
			logger.Debug("updated score parameters successfully")
		}

		// Reset to trigger on the beginning of the next epoch
		timer.Reset(time.Until(nextEpochStartingTime()))
	}
}

// getMaxPeers returns max peers of the given topic.
func (n *p2pNetwork) getMaxPeers(topic string) int {
	if len(topic) == 0 {
		return n.cfg.MaxPeers
	}
	return n.cfg.TopicMaxPeers
}

// peerScore calculates scores for myPeers and returns the ID of least valuable peer.
// The score for a peer is defined as "how valuable this peer would have been if we didn't
// have him, but then connected with" and is a sum of component numbers - each number estimating
// how valuable each peer's subnet to us (via `score` func).
func (n *p2pNetwork) peerScore(peerID peer.ID) float64 {
	const targetPeersPerSubnet = 3

	filterOutPeer := func(peerID peer.ID, peerIDs []peer.ID) []peer.ID {
		if len(peerIDs) == 0 {
			return nil
		}
		result := make([]peer.ID, 0, len(peerIDs))
		for _, elem := range peerIDs {
			if elem == peerID {
				continue
			}
			result = append(result, elem)
		}
		return result
	}

	result := 0.0

	peerSubnets := n.idx.GetPeerSubnets(peerID)
	sharedSubnets := records.SharedSubnets(n.activeSubnets, peerSubnets, 0)
	for _, subnet := range sharedSubnets {
		subnetPeers := n.idx.GetSubnetPeers(subnet)
		thisSubnetPeersExcluding := filterOutPeer(peerID, subnetPeers)
		result += score(float64(targetPeersPerSubnet), float64(len(thisSubnetPeersExcluding)))
	}

	return result
}

// score calculates the score based on the target and input number
func score(target, num float64) float64 {
	const X = 3
	if num < target {
		// Decreasing score as numbers approach the target
		return math.Pow(target-num+1, 1.5) * X
	} else if num == target {
		// Fixed score for exact match
		return X
	} else {
		// Decreasing score for numbers above the target
		return 10 / math.Pow(num-target+1, 1.5)
	}
}
