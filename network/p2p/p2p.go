package p2pv1

import (
	"context"
	"errors"
	"fmt"
	"math/rand"
	"slices"
	"strings"
	"sync/atomic"
	"time"

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

	go n.startDiscovery(logger, connector)

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
			records.Subnets(n.activeSubnets).Clone(),
		)
		if disconnectedCnt > 0 {
			// we can accept more peer connections now, no need to trim
			return
		}

		// maxPeersToDrop value should be in the range of 3-5% of MaxPeers for trimming to work
		// fast enough so that our node finds good set of peers within 10-20 minutes after node
		// start; it shouldn't be too large because that would negatively affect Ethereum duty
		// execution quality
		const maxPeersToDrop = 4 // targeting MaxPeers in 60-90 range

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
			// for with incoming connections - only slightly so), hence we'll only do it 1/3 of the times
			if rand.Intn(3) > 0 {
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
func (n *p2pNetwork) PeerProtection(
	immunityQuota int,
) map[peer.ID]struct{} {
	protectedPeers := make(map[peer.ID]struct{})

	tpcs := n.topicsCtrl.Topics()
	peerz := make(map[string][]peer.ID, len(tpcs)) // topic -> peers
	for _, tpc := range tpcs {
		var err error
		peerz[tpc], err = n.topicsCtrl.Peers(tpc)
		if err != nil {
			n.interfaceLogger.Error(
				"Cant get peers for topic, skipping to keep the network running",
				zap.String("topic", tpc),
				zap.Error(err),
			)
			continue
		}
	}

	sharedTopics := func(peerID peer.ID) int {
		shared := 0
		for _, tpc := range tpcs {
			if slices.Contains(peerz[tpc], peerID) {
				shared++
			}
		}
		return shared
	}

	// for each topic we have peers in sort these peers by the number of topics they share with us
	// and protect top 2 peers for each topic, if there's only one peer in that topic, always protect it
	for _, tpc := range tpcs {
		peersInTopic := peerz[tpc]
		if len(peersInTopic) == 0 {
			continue
		}
		if len(peersInTopic) == 1 {
			protectedPeers[peersInTopic[0]] = struct{}{}
			immunityQuota--
			continue
		}

		slices.SortFunc(peersInTopic, func(a, b peer.ID) int {
			x := sharedTopics(a)
			y := sharedTopics(b)
			return x - y // desc order
		})

		const minPeersPerTopic = 2
		for i := 0; i < min(minPeersPerTopic, len(peersInTopic)) && immunityQuota > 0; i++ {
			protectedPeers[peersInTopic[i]] = struct{}{}
			immunityQuota--
		}
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
