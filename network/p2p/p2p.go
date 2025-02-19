package p2pv1

import (
	"context"
	"errors"
	"fmt"
	"math"
	"math/bits"
	"math/rand"
	"os"
	"slices"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	"github.com/libp2p/go-libp2p/core/connmgr"
	connmgrcore "github.com/libp2p/go-libp2p/core/connmgr"
	"github.com/libp2p/go-libp2p/core/host"
	p2pnet "github.com/libp2p/go-libp2p/core/network"
	"github.com/libp2p/go-libp2p/core/peer"
	libp2pdiscbackoff "github.com/libp2p/go-libp2p/p2p/discovery/backoff"
	ma "github.com/multiformats/go-multiaddr"
	"github.com/oleiade/lane/v2"
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
			n.interfaceLogger.Error("Cant get peers for specified topic", zap.String("topic", tpc), zap.Error(err))
			return nil, nil
		}
	}
	allPeers, err := n.topicsCtrl.Peers("")
	if err != nil {
		n.interfaceLogger.Error("Cant list all peers", zap.Error(err))
		return nil, nil
	}
	return allPeers, peerz
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
	p2pStartTime := time.Now()

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

	connectorProposals := make(chan peer.AddrInfo, connectorQueueSize)
	go n.startDiscovery(logger, connectorProposals)
	go func() {
		// keep discovered peers in a pool so we can choose the best ones
		for proposal := range connectorProposals {
			discoveredPeer := peers.DiscoveredPeer{
				AddrInfo:       proposal,
				ConnectRetries: 0,
			}
			peers.DiscoveredPeersPool.Set(proposal.ID, discoveredPeer)

			n.interfaceLogger.Debug(
				"discovery proposed peer, adding it to the pool",
				zap.String("peer_id", string(proposal.ID)),
			)
		}
	}()
	// start go-routine to choose the best peer(s) from the pool of discovered peers to propose
	// these as outbound connections, all discovered peers are pooled so we can choose to propose
	// the best ones (instead of the ones we've discovered first, for example), additionally when
	// choosing a batch of peers to propose we evaluate "peer synergy" between them - which is a way
	// to pick peers such that they don't have too many overlapping subnets and instead cover a
	// wider range of dead/solo subnets for us
	async.Interval(n.ctx, 15*time.Second, func() {
		// give discovery some time to find the best peers right after node start, the exact
		// best time to wait is arrived at experimentally
		if time.Since(p2pStartTime) < 5*time.Minute {
			return
		}

		// see how many vacant slots (for outbound connections) we have, note that we always
		// prefer outbound connections over inbound and hence we check against MaxPeers and
		// not some outbound-specific limit value (we don't even define any outbound-specific
		// limit)
		inbound, outbound := n.connectionStats()
		vacantOutboundSlotCnt := n.cfg.MaxPeers - (inbound + outbound)
		if vacantOutboundSlotCnt <= 0 {
			n.interfaceLogger.Debug(
				"Not gonna propose discovered peers: ran out of vacant peer slots",
				zap.Int("inbound_peers", inbound),
				zap.Int("outbound_peers", outbound),
				zap.Int("max_peers", n.cfg.MaxPeers),
			)
			return
		}

		// peersByPriority keeps track of best peers (by their peer score)
		peersByPriority := lane.NewMaxPriorityQueue[peers.DiscoveredPeer, float64]()
		peers.DiscoveredPeersPool.Range(func(key peer.ID, value peers.DiscoveredPeer) bool {
			const retryLimit = 2
			if value.ConnectRetries >= retryLimit {
				// this discovered peer has been tried many times already, we'll ignore him but won't
				// remove him from DiscoveredPeersPool since if we do - discovery might suggest this
				// peer again (essentially resetting this peer's retry attempts counter to 0)

				// TODO - comment out
				// this log line is commented out as it is too spammy
				n.interfaceLogger.Debug(
					"Not gonna propose discovered peer: ran out of retries",
					zap.String("peer_id", string(key)),
				)
				return true
			}
			proposalScore := n.peerScore(key)
			if proposalScore <= 0 {
				return true // we are not interested in this peer at all
			}
			peersByPriority.Push(value, proposalScore)
			return true
		})

		// propose only half as many peers as we have outbound slots available because this
		// leaves some vacant slots for the next iteration - on the next iteration better
		// peers might show up (so we don't want to "spend" all of these vacant slots at once)
		peersToProposeCnt := max(vacantOutboundSlotCnt/2, 1)
		// also limit how many peers we want to propose with respect to "peer synergy" (this
		// value can't be too high for performance reasons, 12 seems like a good middle-ground)
		const peersToProposeMaxWithSynergy = 12
		peersToProposeCnt = min(peersToProposeCnt, peersToProposeMaxWithSynergy)
		// peersToProposePoolCnt is a size of candidate-peers pool we'll be choosing exactly
		// peersToProposeCnt peers from
		peersToProposePoolCnt := 2 * peersToProposeCnt

		// additionally, make sure we don't exceed peersByPriority queue size
		peersToProposeCnt = min(peersToProposeCnt, int(peersByPriority.Size()))         // nolint: gosec
		peersToProposePoolCnt = min(peersToProposePoolCnt, int(peersByPriority.Size())) // nolint: gosec

		// terminate early if there is no peers to propose
		if peersToProposeCnt < 1 {
			n.interfaceLogger.Info("Not gonna propose discovered peers: no suitable peer candidates")
			return
		}

		// prepare a pool of peers we'll be choosing best-synergy-peers from
		peersToProposePoolDesc := make([]peers.DiscoveredPeer, 0, peersToProposePoolCnt)
		minScore, maxScore := math.MaxFloat64, 0.0 // used for printing debugging info
		for i := 0; i < peersToProposePoolCnt; i++ {
			peerCandidate, priority, _ := peersByPriority.Pop()
			if minScore > priority {
				minScore = priority
			}
			if maxScore < priority {
				maxScore = priority
			}
			peersToProposePoolDesc = append(peersToProposePoolDesc, peerCandidate)
		}

		// SubnetSum represents a sum of 0 or more subnets, each byte at index K counts how many
		// of that particular subnet at index K the summed subnet-sets have
		type SubnetSum [commons.SubnetsCount]byte
		// addSubnets combines two sums to calculate the resulting subnet sum
		addSubnets := func(a SubnetSum, b SubnetSum) SubnetSum {
			result := SubnetSum{}
			for i := 0; i < commons.SubnetsCount; i++ {
				result[i] = a[i] + b[i]
			}
			return result
		}

		// ownSubnetSum represents subnet sum of peers we already have open connections with
		ownSubnetSum := SubnetSum{}
		allPeerIDs, err := n.topicsCtrl.Peers("")
		if err != nil {
			n.interfaceLogger.Error("Cant list all peers", zap.Error(err))
			return
		}
		for _, pID := range allPeerIDs {
			pSubnets := n.idx.GetPeerSubnets(pID)
			ownSubnetSum = addSubnets(ownSubnetSum, SubnetSum(pSubnets))
		}
		// scoreSubnetSumSynergy is same as scorePeerSet but for subnet sum
		scoreSubnetSumSynergy := func(s SubnetSum) (score float64, highestPossible bool) {
			const deadPriorityMultiplier = 5 // we value resolving dead subnets 5x higher over resolving solo subnets
			const maxPossibleScore = commons.SubnetsCount * deadPriorityMultiplier

			highestPossible = true
			deadSubnetCnt := 0 // how many dead subnets SubnetSum has
			soloSubnetCnt := 0 // how many solo subnets SubnetSum has
			for i := 0; i < commons.SubnetsCount; i++ {
				if s[i] == 0 {
					deadSubnetCnt++
					highestPossible = false
				}
				if s[i] == 1 {
					soloSubnetCnt++
					highestPossible = false
				}
			}
			score = float64(maxPossibleScore - (deadPriorityMultiplier*deadSubnetCnt + soloSubnetCnt))
			return score, highestPossible
		}
		// scorePeerSet estimates how good/bad a peerSet is based on how many unhealthy
		// (we are mostly really interested in dead and solo subnets) subnets we still have
		// once we've connected this set of peers
		scorePeerSet := func(peerSet uint64, allPeersDesc []peers.DiscoveredPeer) (score float64, highestPossible bool) {
			// totalSubnetSum will represent a subnet sum of our own subnets + all the subnets in this candidate
			// peer-set, it will be used to estimate how "valuable" this candidate peer-set is to us (so we can
			// find the best peer-set)
			totalSubnetSum := ownSubnetSum
			for peerIdx, peerMask := 0, uint64(1); peerIdx < len(allPeersDesc); peerIdx, peerMask = peerIdx+1, peerMask<<1 {
				peerPresentInCandidateSet := peerSet & peerMask
				if peerPresentInCandidateSet == uint64(0) {
					continue // this peerIdx isn't part of candidate peer-set
				}
				p := allPeersDesc[peerIdx]
				pSubnets := n.idx.GetPeerSubnets(p.ID)
				totalSubnetSum = addSubnets(totalSubnetSum, SubnetSum(pSubnets))
			}
			return scoreSubnetSumSynergy(totalSubnetSum)
		}
		// peerSetToPeerList picks peers peerSet describes out of allPeers compiling them into new slice
		peerSetToPeerList := func(peerSet uint64, allPeers []peers.DiscoveredPeer) []peers.DiscoveredPeer {
			result := make([]peers.DiscoveredPeer, 0, peersToProposeCnt)
			for peerIdx, peerMask := 0, uint64(1); peerIdx < len(allPeers); peerIdx, peerMask = peerIdx+1, peerMask<<1 {
				peerPresentInCandidateSet := peerSet & peerMask
				if peerPresentInCandidateSet == uint64(0) {
					continue // this peerIdx isn't part of candidate peer-set
				}
				p := allPeers[peerIdx]
				result = append(result, p)
			}
			return result
		}

		// now we can identify the best synergy peer-set, but first check if we even need to
		// brute-force search for it - we don't if top peersToProposeCnt peers already give us
		// the highest possible score
		topPeerSet := uint64(0)
		for peerIdx, peerMask := 0, uint64(1); peerIdx < peersToProposeCnt; peerIdx, peerMask = peerIdx+1, peerMask<<1 {
			topPeerSet |= peerMask
		}
		var (
			bestSynergyScore     float64
			bestSynergyPeers     []peers.DiscoveredPeer
			skipBruteForceSearch bool
		)
		topSynergyScore, highestPossibleScore := scorePeerSet(topPeerSet, peersToProposePoolDesc)
		if highestPossibleScore {
			bestSynergyScore = topSynergyScore
			bestSynergyPeers = peerSetToPeerList(topPeerSet, peersToProposePoolDesc)
			skipBruteForceSearch = true // no reason to brute-force search
		}
		// iterate over all possible peer-sets of size peersToProposePoolCnt that can be generated from
		// peersToProposePoolDesc (a peer-set is represented by uint64 number, each 1-value bit represents
		// peer presence while each 0-bit represents peer absence in such peer-set) and find a peer-set
		// of size peersToProposeCnt that has peers with the best synergy
		//
		// peersToProposePoolLastSet is the last peer-set represented by 2^peersToProposePoolCnt
		peersToProposePoolLastSet := uint64(1) << peersToProposePoolCnt
		for candidateSet := uint64(0); !skipBruteForceSearch && candidateSet < peersToProposePoolLastSet; candidateSet++ {
			// we are only interested in peer-set that have exactly peersToProposeCnt peers in them
			candidateSetSize := bits.OnesCount64(candidateSet)
			if candidateSetSize != peersToProposeCnt {
				continue // not interested in this peer-set
			}
			tmpSynergyScore, highestPossibleScore := scorePeerSet(candidateSet, peersToProposePoolDesc)
			if highestPossibleScore || tmpSynergyScore > bestSynergyScore {
				bestSynergyScore = tmpSynergyScore
				bestSynergyPeers = peerSetToPeerList(candidateSet, peersToProposePoolDesc)
			}
			if highestPossibleScore {
				break // no reason to search further
			}
		}

		// finally, offer best-synergy peers we came up with to connector so it tries to connect these
		for _, p := range bestSynergyPeers {
			// update retry counter for this peer (so we eventually skip it after certain number of retries)
			peers.DiscoveredPeersPool.Set(p.ID, peers.DiscoveredPeer{
				AddrInfo:       p.AddrInfo,
				ConnectRetries: p.ConnectRetries + 1,
			})
			connector <- p.AddrInfo
		}
		n.interfaceLogger.Info(
			"Proposed discovered peers",
			zap.Int("count", peersToProposeCnt),
			zap.Float64("min_score", minScore),
			zap.Float64("max_score", maxScore),
		)
	})

	async.Interval(n.ctx, peersTrimmingInterval, n.peersTrimming(logger))

	async.Interval(n.ctx, peersReportingInterval, recordPeerCount(n.ctx, logger, n.host))

	async.Interval(n.ctx, peerIdentitiesReportingInterval, recordPeerIdentities(n.ctx, n.host, n.idx))

	async.Interval(n.ctx, topicsReportingInterval, recordPeerCountPerTopic(n.ctx, logger, n.topicsCtrl, 2))

	// TODO - used for testing (to gather more stats on how logs node takes to resolve dead
	// subnets at node-start)
	async.Interval(n.ctx, 1*time.Hour, func() {
		n.interfaceLogger.Info("FORCE-restarting SSV node")
		os.Exit(0)
	})

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
		const maxPeersToDrop = 4 // targeting MaxPeers in 60-90 range

		protectEveryOutbound := false

		// see if we can accept more peer connections already (no need to trim), note we trim not
		// only when our current connections reach MaxPeers limit exactly but even if we get close
		// enough to it - this ensures we don't skip trim iteration because of "random fluctuations"
		// in currently connected peer count at that limit boundary
		connectedPeers = n.host.Network().Peers()
		if len(connectedPeers) <= n.cfg.MaxPeers-maxPeersToDrop {
			// we probably don't want to trim then

			// additionally, make sure incoming connections aren't at the limit - since if they are we
			// actually might want to trim some of them to make sure we re-cycle incoming connections
			// at least occasionally (note btw, with current implementation there is no guarantee incoming
			// connections will be trimmed in this case, since we don't differentiate between incoming/outgoing
			// when trimming)
			in, _ := n.connectionStats()
			if in < n.inboundLimit() {
				return // skip trim iteration
			}
			// we don't want to trim incoming connections as often as outgoing connections (since trimming
			// outgoing connections often helps us discover valuable peers, while it's not really the case
			// with incoming connections - only slightly so), hence we'll only do it 1/5 of the times
			if rand.Intn(5) > 0 { // nolint: gosec
				return // skip trim iteration
			}

			// we decided to trim then but only because we want to rotate some incoming connections, we'd
			// want to protect all our outgoing connections then since we don't have enough of these
			protectEveryOutbound = true
		}

		// gotta trim some peers then
		immunityQuota := len(connectedPeers) - maxPeersToDrop
		protectedPeers := n.PeerProtection(immunityQuota, protectEveryOutbound)
		for _, p := range connectedPeers {
			if _, ok := protectedPeers[p]; ok {
				n.libConnManager.Protect(p, peers.ProtectedTag)
				continue
			}
			n.libConnManager.Unprotect(p, peers.ProtectedTag)
		}
		connMgr.TrimPeers(ctx, logger, n.host.Network(), maxPeersToDrop) // trim up to maxPeersToDrop
	}
}

// PeerProtection returns a map of protected peers based on how valuable those peers to us are,
// peer value is proportional to how much of valuable subnets (dead/solo/duo) he contributes, as
// defined by peerScore func.
// Param immunityQuota limits how many peers can be protected at most, it is distributed evenly
// between inbound and outbound connections to make sure we don't overly protect one connection
// type (because if we do it can result in connections of other type not getting trimmed frequently
// enough to be replaced by better connection-candidates).
// Param protectEveryOutbound signals that we want to protect every outbound connection we have
// disregarding immunityQuota entirely (when it comes to outbound connections).
func (n *p2pNetwork) PeerProtection(immunityQuota int, protectEveryOutbound bool) map[peer.ID]struct{} {
	myPeersSet := make(map[peer.ID]struct{})
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

	immunityQuotaInbound := immunityQuota / 2
	immunityQuotaOutbound := immunityQuota - immunityQuotaInbound

	protectedPeers := make(map[peer.ID]struct{})
	for _, p := range myPeers {
		if immunityQuotaInbound == 0 && immunityQuotaOutbound == 0 {
			break // can't protect any more peers since we reached our quotas
		}
		pConns := n.host.Network().ConnsToPeer(p)
		// we shouldn't have more than 1 connection per peer, but if we do we'd want
		// a warning about it logged, and we'd want to handle it to the best of our ability
		if len(pConns) > 1 {
			n.interfaceLogger.Error(
				"PeerProtection: encountered peer we have multiple open connections with (expected 1 at most)",
				zap.String("peer_id", p.String()),
				zap.Int("connections_count", len(pConns)),
			)
		}
		for _, pConn := range pConns {
			connDir := pConn.Stat().Direction
			if connDir == p2pnet.DirUnknown {
				n.interfaceLogger.Error(
					"PeerProtection: encountered peer connection with direction Unknown",
					zap.String("peer_id", p.String()),
				)
				continue
			}
			if connDir == p2pnet.DirInbound {
				if immunityQuotaInbound > 0 {
					protectedPeers[p] = struct{}{}
					immunityQuotaInbound--
				}
			}
			if connDir == p2pnet.DirOutbound {
				if protectEveryOutbound {
					protectedPeers[p] = struct{}{}
				} else if immunityQuotaOutbound > 0 {
					immunityQuotaOutbound--
					protectedPeers[p] = struct{}{}
				}
			}
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
	registeredSubnets := make([]byte, commons.SubnetsCount)
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
			self.Metadata.Subnets = commons.Subnets(n.activeSubnets).String()
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

		allSubs, _ := commons.Subnets{}.FromString(commons.AllSubnets)
		subnetsList := commons.SharedSubnets(allSubs, n.activeSubnets, 0)
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

// peerScore calculates a score for peerID based on how valuable this peer's contribution
// to us assessing each subnet-contribution he makes (as estimated by score func).
func (n *p2pNetwork) peerScore(peerID peer.ID) float64 {
	result := 0.0

	peerSubnets := n.idx.GetPeerSubnets(peerID)
	sharedSubnets := commons.SharedSubnets(n.activeSubnets, peerSubnets, 0)
	for _, subnet := range sharedSubnets {
		result += n.score(peerID, subnet)
	}

	return result
}

// score assesses how valuable the contribution of peerID to specified subnet is by calculating
// how valuable this peer would have been if we didn't have him, but then connected with.
func (n *p2pNetwork) score(peerID peer.ID, subnet int) float64 {
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

	topic := strconv.Itoa(subnet)
	subnetPeers, err := n.topicsCtrl.Peers(topic)
	if err != nil {
		n.interfaceLogger.Debug(
			"cannot score peer with respect to this subnet, assuming zero contribution",
			zap.String("topic", topic),
			zap.Error(fmt.Errorf("could not get topic peers: %w", err)),
		)
		return 0.0
	}
	subnetPeersExcluding := len(filterOutPeer(peerID, subnetPeers))

	const targetPeersPerSubnet = 3
	return score(targetPeersPerSubnet, subnetPeersExcluding)
}

func score(desired, actual int) float64 {
	if actual > desired {
		return float64(desired) / float64(actual) // is always less than 1.0
	}
	if actual == desired {
		return 2.0 // at least 2x better than when `actual > desired`
	}
	// make every unit of difference count, starting with the score of 2.0 (when `actual == desired`)
	// and increasing exponentially
	diff := desired - actual
	result := 2.0
	for i := 1; i <= diff; i++ {
		result *= float64(2.0 + i)
	}
	return result
}
