package p2pv1

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"math/rand"
	"slices"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	connmgrcore "github.com/libp2p/go-libp2p/core/connmgr"
	"github.com/libp2p/go-libp2p/core/host"
	p2pnet "github.com/libp2p/go-libp2p/core/network"
	"github.com/libp2p/go-libp2p/core/peer"
	libp2pdiscbackoff "github.com/libp2p/go-libp2p/p2p/discovery/backoff"
	ma "github.com/multiformats/go-multiaddr"
	"go.uber.org/zap"

	"github.com/ssvlabs/ssv/ssvsigner/keys"

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
	operatorstorage "github.com/ssvlabs/ssv/operator/storage"
	"github.com/ssvlabs/ssv/utils/async"
	"github.com/ssvlabs/ssv/utils/hashmap"
	"github.com/ssvlabs/ssv/utils/tasks"
	"github.com/ssvlabs/ssv/utils/ttl"
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
	peersTrimmingInterval           = 30 * time.Second
	peersReportingInterval          = 60 * time.Second
	peerIdentitiesReportingInterval = 5 * time.Minute
	topicsReportingInterval         = 60 * time.Second
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

	logger *zap.Logger
	cfg    *Config

	host         host.Host
	streamCtrl   streams.StreamController
	idx          peers.Index
	isIdxSet     atomic.Bool
	disc         discovery.Service
	topicsCtrl   topics.Controller
	msgRouter    network.MessageRouter
	msgResolver  topics.MsgPeersResolver
	msgValidator validation.MessageValidator
	connHandler  connections.ConnHandler
	connGater    connmgrcore.ConnectionGater
	trustedPeers []*peer.AddrInfo

	state int32

	activeCommittees *hashmap.Map[string, validatorStatus]

	backoffConnector *libp2pdiscbackoff.BackoffConnector

	// persistentSubnets holds subnets on node startup,
	// these subnets should not be unsubscribed from even if all validators associated with them are removed
	persistentSubnets commons.Subnets
	// currentSubnets holds current subnets which depend on current active validators and committees
	currentSubnets commons.Subnets

	libConnManager connmgrcore.ConnManager

	nodeStorage             operatorstorage.Storage
	operatorPKHashToPKCache *hashmap.Map[string, []byte] // used for metrics
	operatorSigner          keys.OperatorSigner
	operatorDataStore       operatordatastore.OperatorDataStore

	// discoveredPeersPool keeps track of recently discovered peers so we can rank them and choose
	// the best candidates to connect to.
	discoveredPeersPool *ttl.Map[peer.ID, discovery.DiscoveredPeer]
	// trimmedRecently keeps track of recently trimmed peers so we don't try to connect to these
	// shortly after we've trimmed these (we still might consider connecting to these once they
	// are removed from this map after some time passes)
	trimmedRecently *ttl.Map[peer.ID, struct{}]
}

// New creates a new p2p network
func New(
	logger *zap.Logger,
	cfg *Config,
) (*p2pNetwork, error) {
	ctx, cancel := context.WithCancel(cfg.Ctx)

	n := &p2pNetwork{
		parentCtx:               cfg.Ctx,
		ctx:                     ctx,
		cancel:                  cancel,
		logger:                  logger.Named(logging.NameP2PNetwork),
		cfg:                     cfg,
		msgRouter:               cfg.Router,
		msgValidator:            cfg.MessageValidator,
		state:                   stateClosed,
		activeCommittees:        hashmap.New[string, validatorStatus](),
		nodeStorage:             cfg.NodeStorage,
		operatorPKHashToPKCache: hashmap.New[string, []byte](),
		operatorSigner:          cfg.OperatorSigner,
		operatorDataStore:       cfg.OperatorDataStore,
		discoveredPeersPool:     ttl.New[peer.ID, discovery.DiscoveredPeer](30*time.Minute, 3*time.Minute),
		trimmedRecently:         ttl.New[peer.ID, struct{}](30*time.Minute, 3*time.Minute),
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

// Peers returns all peers we are connected to
func (n *p2pNetwork) Peers() []peer.ID {
	allPeers, err := n.topicsCtrl.Peers("")
	if err != nil {
		n.logger.Error("Cant list all peers", zap.Error(err))
		return nil
	}
	return allPeers
}

// PeersByTopic returns topic->peers mapping for all peers we are connected to
func (n *p2pNetwork) PeersByTopic() map[string][]peer.ID {
	tpcs := n.topicsCtrl.Topics()
	peerz := make(map[string][]peer.ID, len(tpcs))
	for _, tpc := range tpcs {
		peers, err := n.topicsCtrl.Peers(tpc)
		if err != nil {
			n.logger.Error("Cant get peers for specified topic", zap.String("topic", tpc), zap.Error(err))
			return nil
		}
		peerz[tpc] = peers
	}
	return peerz
}

// Close implements io.Closer
func (n *p2pNetwork) Close() error {
	atomic.SwapInt32(&n.state, stateClosing)
	defer atomic.StoreInt32(&n.state, stateClosed)
	n.cancel()
	if err := n.libConnManager.Close(); err != nil {
		n.logger.Warn("could not close discovery", zap.Error(err))
	}
	if err := n.disc.Close(); err != nil {
		n.logger.Warn("could not close discovery", zap.Error(err))
	}
	if err := n.idx.Close(); err != nil {
		n.logger.Warn("could not close index", zap.Error(err))
	}
	if err := n.topicsCtrl.Close(); err != nil {
		n.logger.Warn("could not close topics controller", zap.Error(err))
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
func (n *p2pNetwork) Start() error {
	if atomic.SwapInt32(&n.state, stateReady) == stateReady {
		// return errors.New("could not setup network: in ready state")
		return nil
	}

	pAddrs, err := peer.AddrInfoToP2pAddrs(&peer.AddrInfo{
		ID:    n.host.ID(),
		Addrs: n.host.Addrs(),
	})
	if err != nil {
		n.logger.Fatal("could not get my address", zap.Error(err))
	}
	maStrs := make([]string, len(pAddrs))
	for i, ima := range pAddrs {
		maStrs[i] = ima.String()
	}
	n.logger.Info("starting p2p",
		zap.String("my_address", strings.Join(maStrs, ",")),
		zap.Int("trusted_peers", len(n.trustedPeers)),
	)

	err = n.startDiscovery()
	if err != nil {
		return fmt.Errorf("could not start discovery: %w", err)
	}

	async.Interval(n.ctx, peersTrimmingInterval, n.peersTrimming())

	async.Interval(n.ctx, peersReportingInterval, recordPeerCount(n.ctx, n.logger, n.host))

	async.Interval(n.ctx, peerIdentitiesReportingInterval, recordPeerIdentities(n.ctx, n.host, n.idx))

	async.Interval(n.ctx, topicsReportingInterval, recordPeerCountPerTopic(n.ctx, n.logger, n.topicsCtrl, 2))

	if err := n.subscribeToFixedSubnets(); err != nil {
		return err
	}

	return nil
}

// Returns a function that trims currently connected peers if necessary, namely:
//   - dropping peers with bad gossip score
//   - dropping irrelevant peers that don't have any subnet in common with us
//   - (when we are close to MaxPeers limit) dropping several peers with the worst score
//     which is based on how many valuable (dead/solo/duo) subnets a peer contributes
//   - (when Inbound peers are close to its limit) dropping several Inbound peers with
//     the worst score
func (n *p2pNetwork) peersTrimming() func() {
	return func() {
		ctx, cancel := context.WithTimeout(n.ctx, 60*time.Second)
		defer cancel()
		defer func() {
			_ = n.idx.GetSubnetsStats() // collect metrics
		}()

		connMgr := peers.NewConnManager(n.logger, n.libConnManager, n.idx, n.idx)

		disconnectedCnt := connMgr.DisconnectFromBadPeers(n.host.Network(), n.host.Network().Peers())
		if disconnectedCnt > 0 {
			// we can accept more peer connections now, no need to trim
			return
		}

		connectedPeers := n.host.Network().Peers()

		const maximumIrrelevantPeersToDisconnect = 3
		disconnectedCnt = connMgr.DisconnectFromIrrelevantPeers(
			maximumIrrelevantPeersToDisconnect,
			n.host.Network(),
			connectedPeers,
			n.currentSubnets,
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

		trimInboundOnly := false

		// see if we can accept more peer connections already (no need to trim), note we trim not
		// only when our current connections reach MaxPeers limit exactly but even if we get close
		// enough to it - this ensures we don't skip trim iteration because of "random fluctuations"
		// in currently connected peer count at that limit boundary
		connectedPeers = n.host.Network().Peers()
		if len(connectedPeers) <= n.cfg.MaxPeers-maxPeersToDrop {
			// We probably don't want to trim outgoing connections then, but from time-to-time we want to
			// trim (and rotate) some incoming connections when inbound limit is hit just to make sure
			// inbound connections are rotated occasionally in reliable manner.
			// Note, we don't want to trim incoming connections as often as outgoing connections (since
			// trimming outgoing connections often helps us discover valuable peers, while it's not really
			// the case with incoming connections - only slightly so) hence sometimes we randomly skip this
			in, _ := n.connectionStats()
			if in < n.inboundLimit() {
				return // skip trim iteration
			}
			if rand.Intn(5) > 0 { // nolint: gosec
				return // skip trim iteration
			}
			trimInboundOnly = true
		}

		inboundBefore, outboundBefore := n.connectionStats()
		peersToTrim := n.choosePeersToTrim(maxPeersToDrop, trimInboundOnly)
		connMgr.TrimPeers(ctx, n.host.Network(), peersToTrim)
		for pid := range peersToTrim {
			n.trimmedRecently.Set(pid, struct{}{})
		}
		inboundAfter, outboundAfter := n.connectionStats()
		n.logger.Debug(
			"trimmed peers",
			zap.Int("inbound_peers_before_trim", inboundBefore),
			zap.Int("outbound_peers_before_trim", outboundBefore),
			zap.Int("inbound_peers_after_trim", inboundAfter),
			zap.Int("outbound_peers_after_trim", outboundAfter),
			zap.Any("trimmed_peers", maps.Keys(peersToTrim)),
		)
	}
}

// choosePeersToTrim returns a map of peers that are least-valuable to us based on how much
// (dead/solo/duo) they contribute to us (as defined by peerScore func).
func (n *p2pNetwork) choosePeersToTrim(trimCnt int, trimInboundOnly bool) map[peer.ID]struct{} {
	myPeers, err := n.topicsCtrl.Peers("")
	if err != nil {
		n.logger.Error("Cant get all of our peers", zap.Error(err))
		return nil
	}

	slices.SortFunc(myPeers, func(a, b peer.ID) int {
		// sort in asc order (peers with the lowest scores come first)
		aScore := n.peerScore(a)
		bScore := n.peerScore(b)
		if aScore < bScore {
			return -1
		}
		if aScore > bScore {
			return 1
		}
		return 0
	})

	result := make(map[peer.ID]struct{}, trimCnt)
	for _, p := range myPeers {
		if trimCnt <= 0 {
			break
		}
		pConns := n.host.Network().ConnsToPeer(p)
		// we shouldn't have more than 1 connection per peer, but if we do we'd want a
		// warning about it logged, and we'd want to handle it to the best of our ability
		if len(pConns) > 1 {
			n.logger.Error(
				"choosePeersToTrim: encountered peer we have multiple open connections with (expected 1 at most)",
				zap.String("peer_id", p.String()),
				zap.Int("connections_count", len(pConns)),
			)
		}
		for _, pConn := range pConns {
			connDir := pConn.Stat().Direction
			if connDir == p2pnet.DirUnknown {
				n.logger.Error(
					"choosePeersToTrim: encountered peer connection with direction Unknown",
					zap.String("peer_id", p.String()),
				)
			}
			if connDir == p2pnet.DirOutbound && trimInboundOnly {
				continue
			}
			result[p] = struct{}{}
			trimCnt--
		}
	}
	return result
}

// bootstrapDiscovery starts the required services
// it will try to bootstrap discovery service, and inject a connect function.
// the connect function checks if we can connect to the given peer and if so passing it to the backoff connector.
func (n *p2pNetwork) bootstrapDiscovery(connector chan peer.AddrInfo) {
	err := tasks.Retry(func() error {
		return n.disc.Bootstrap(func(e discovery.PeerEvent) {
			if err := n.idx.CanConnect(e.AddrInfo.ID); err != nil {
				n.logger.Debug("skipping new peer", fields.PeerID(e.AddrInfo.ID), zap.Error(err))
				return
			}
			select {
			case connector <- e.AddrInfo:
			default:
				n.logger.Warn("connector queue is full, skipping new peer", fields.PeerID(e.AddrInfo.ID))
			}
		})
	}, 3)
	if err != nil {
		n.logger.Panic("could not setup discovery", zap.Error(err))
	}
}

func (n *p2pNetwork) isReady() bool {
	return atomic.LoadInt32(&n.state) == stateReady
}

// UpdateSubnets will update the registered subnets according to active validators
// NOTE: it won't subscribe to the subnets (use subscribeToFixedSubnets for that)
func (n *p2pNetwork) UpdateSubnets() {
	// TODO: this is a temporary fix to update subnets when validators are added/removed,
	// there is a pending PR to replace this: https://github.com/ssvlabs/ssv/pull/990
	ticker := time.NewTicker(time.Second)
	registeredSubnets := commons.Subnets{}
	defer ticker.Stop()

	// Run immediately and then every second.
	for ; true; <-ticker.C {
		start := time.Now()

		updatedSubnets := n.SubscribedSubnets()
		n.currentSubnets = updatedSubnets

		// Compute the not yet registered subnets.
		addedSubnets := make([]uint64, 0)
		subnetList := updatedSubnets.SubnetList()
		for _, subnet := range subnetList {
			if !registeredSubnets.IsSet(subnet) {
				addedSubnets = append(addedSubnets, subnet)
			}
		}

		// Compute the not anymore registered subnets.
		removedSubnets := make([]uint64, 0)
		subnetList = registeredSubnets.SubnetList()
		for _, subnet := range subnetList {
			if !updatedSubnets.IsSet(subnet) {
				removedSubnets = append(removedSubnets, subnet)
			}
		}

		registeredSubnets = updatedSubnets

		if len(addedSubnets) == 0 && len(removedSubnets) == 0 {
			continue
		}

		n.idx.UpdateSelfRecord(func(self *records.NodeInfo) *records.NodeInfo {
			self.Metadata.Subnets = n.currentSubnets.String()
			return self
		})

		// Register/unregister subnets for discovery.
		var errs error
		var hasAdded, hasRemoved bool
		if len(addedSubnets) > 0 {
			var err error
			hasAdded, err = n.disc.RegisterSubnets(addedSubnets...)
			if err != nil {
				n.logger.Debug("could not register subnets", zap.Error(err))
				errs = errors.Join(errs, err)
			}
		}
		if len(removedSubnets) > 0 {
			var err error
			hasRemoved, err = n.disc.DeregisterSubnets(removedSubnets...)
			if err != nil {
				n.logger.Debug("could not unregister subnets", zap.Error(err))
				errs = errors.Join(errs, err)
			}

			// Unsubscribe from the removed subnets.
			for _, removedSubnet := range removedSubnets {
				if err := n.unsubscribeSubnet(removedSubnet); err != nil {
					n.logger.Debug("could not unsubscribe from subnet", zap.Uint64("subnet", removedSubnet), zap.Error(err))
					errs = errors.Join(errs, err)
				} else {
					n.logger.Debug("unsubscribed from subnet", zap.Uint64("subnet", removedSubnet))
				}
			}
		}
		if hasAdded || hasRemoved {
			go n.disc.PublishENR()
		}

		subnetsList := commons.AllSubnets.SharedSubnets(n.currentSubnets)
		n.logger.Debug("updated subnets",
			zap.Any("added", addedSubnets),
			zap.Any("removed", removedSubnets),
			zap.Any("subnets", subnetsList),
			zap.Any("subscribed_topics", n.topicsCtrl.Topics()),
			zap.Int("total_subnets", len(subnetsList)),
			fields.Took(time.Since(start)),
			zap.Error(errs),
		)
	}
}

// UpdateScoreParams updates the scoring parameters once per epoch through the call of n.topicsCtrl.UpdateScoreParams
func (n *p2pNetwork) UpdateScoreParams() {
	// TODO: this is a temporary solution to update the score parameters periodically.
	// But, we should use an appropriate trigger for the UpdateScoreParams function that should be
	// called once a validator is added or removed from the network

	// function to get the starting time of the next epoch
	nextEpochStartingTime := func() time.Time {
		currEpoch := n.cfg.BeaconConfig.EstimatedCurrentEpoch()
		nextEpoch := currEpoch + 1
		return n.cfg.BeaconConfig.EpochStartTime(nextEpoch)
	}

	// Create timer that triggers on the beginning of the next epoch
	timer := time.NewTimer(time.Until(nextEpochStartingTime()))
	defer timer.Stop()

	// Run immediately and then once every epoch
	for ; true; <-timer.C {

		// Update score parameters
		err := n.topicsCtrl.UpdateScoreParams()
		if err != nil {
			n.logger.Debug("score parameters update failed", zap.Error(err))
		} else {
			n.logger.Debug("updated score parameters successfully")
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

// peerScore calculates peer score based on how valuable this peer would have been if we didn't
// have him, but then connected with.
func (n *p2pNetwork) peerScore(peerID peer.ID) float64 {
	// Compute number of peers we're connected to for each subnet excluding peer with peerID.
	subnetPeersExcluding := SubnetPeers{}
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
		for _, pID := range peers {
			if pID == peerID {
				continue
			}
			subnetPeersExcluding[subnet]++
		}
	}

	ownSubnets := n.SubscribedSubnets()
	peerSubnets, _ := n.PeersIndex().GetPeerSubnets(peerID)
	return subnetPeersExcluding.Score(ownSubnets, peerSubnets)
}

// SubnetPeers contains the number of peers we are connected to for each subnet.
type SubnetPeers [commons.SubnetsCount]uint16

func (a SubnetPeers) Add(b SubnetPeers) SubnetPeers {
	var sum SubnetPeers
	for i := range a {
		sum[i] = a[i] + b[i]
	}
	return sum
}

// Score estimates how many valuable subnets the given peer would contribute.
// Param ours defines subnets we are interested in.
// Param theirs defines subnets given peer has to offer.
func (a SubnetPeers) Score(ours, theirs commons.Subnets) float64 {
	const (
		duoSubnetPriority  = 1
		soloSubnetPriority = 4
		deadSubnetPriority = 16
	)
	score := float64(0)

	for i := range a {
		// #nosec G115 -- subnet index is never negative
		if ours.IsSet(uint64(i)) && theirs.IsSet(uint64(i)) {
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
			_, _ = fmt.Fprintf(&b, "%d:%d ", i, v)
		}
	}
	return b.String()
}
