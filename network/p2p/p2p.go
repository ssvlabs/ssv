package p2pv1

import (
	"bytes"
	"context"
	"sync"
	"sync/atomic"
	"time"

	"github.com/bloxapp/ssv/logging"
	"github.com/bloxapp/ssv/logging/fields"
	"github.com/cornelk/hashmap"

	connmgrcore "github.com/libp2p/go-libp2p/core/connmgr"
	"github.com/libp2p/go-libp2p/core/host"
	"github.com/libp2p/go-libp2p/core/peer"
	libp2pdiscbackoff "github.com/libp2p/go-libp2p/p2p/discovery/backoff"
	"go.uber.org/zap"

	"github.com/bloxapp/ssv/network"
	"github.com/bloxapp/ssv/network/discovery"
	"github.com/bloxapp/ssv/network/forks"
	forksfactory "github.com/bloxapp/ssv/network/forks/factory"
	"github.com/bloxapp/ssv/network/peers"
	"github.com/bloxapp/ssv/network/peers/connections"
	"github.com/bloxapp/ssv/network/records"
	"github.com/bloxapp/ssv/network/streams"
	"github.com/bloxapp/ssv/network/syncing"
	"github.com/bloxapp/ssv/network/topics"
	operatorstorage "github.com/bloxapp/ssv/operator/storage"
	"github.com/bloxapp/ssv/utils/async"
	"github.com/bloxapp/ssv/utils/tasks"
)

// network states
const (
	stateInitializing int32 = 0
	stateClosing      int32 = 1
	stateClosed       int32 = 2
	stateReady        int32 = 10
)

const (
	connManagerGCInterval           = time.Minute
	connManagerGCTimeout            = time.Minute
	peerIndexGCInterval             = 15 * time.Minute
	peersReportingInterval          = 60 * time.Second
	peerIdentitiesReportingInterval = 5 * time.Minute
	topicsReportingInterval         = 180 * time.Second
)

// p2pNetwork implements network.P2PNetwork
type p2pNetwork struct {
	parentCtx context.Context
	ctx       context.Context
	cancel    context.CancelFunc

	interfaceLogger *zap.Logger // struct logger to log in interface methods that do not accept a logger
	fork            forks.Fork
	cfg             *Config

	host        host.Host
	streamCtrl  streams.StreamController
	idx         peers.Index
	disc        discovery.Service
	topicsCtrl  topics.Controller
	msgRouter   network.MessageRouter
	msgResolver topics.MsgPeersResolver
	connHandler connections.ConnHandler

	state int32

	activeValidators *hashmap.Map[string, validatorStatus]

	backoffConnector *libp2pdiscbackoff.BackoffConnector
	subnets          []byte
	libConnManager   connmgrcore.ConnManager
	syncer           syncing.Syncer
	nodeStorage      operatorstorage.Storage
	operatorPKCache  sync.Map
}

// New creates a new p2p network
func New(logger *zap.Logger, cfg *Config) network.P2PNetwork {
	ctx, cancel := context.WithCancel(cfg.Ctx)

	logger = logger.Named(logging.NameP2PNetwork)

	return &p2pNetwork{
		parentCtx:        cfg.Ctx,
		ctx:              ctx,
		cancel:           cancel,
		interfaceLogger:  logger,
		fork:             forksfactory.NewFork(cfg.ForkVersion),
		cfg:              cfg,
		msgRouter:        cfg.Router,
		state:            stateClosed,
		activeValidators: hashmap.New[string, validatorStatus](),
		nodeStorage:      cfg.NodeStorage,
		operatorPKCache:  sync.Map{},
	}
}

// Host implements HostProvider
func (n *p2pNetwork) Host() host.Host {
	return n.host
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

// Start starts the discovery service, garbage collector (peer index), and reporting.
func (n *p2pNetwork) Start(logger *zap.Logger) error {
	logger = logger.Named(logging.NameP2PNetwork)

	if atomic.SwapInt32(&n.state, stateReady) == stateReady {
		// return errors.New("could not setup network: in ready state")
		return nil
	}

	logger.Info("starting")

	go n.startDiscovery(logger)

	async.Interval(n.ctx, connManagerGCInterval, n.peersBalancing(logger))

	async.Interval(n.ctx, peerIndexGCInterval, n.idx.GC)

	async.Interval(n.ctx, peersReportingInterval, n.reportAllPeers(logger))

	async.Interval(n.ctx, peerIdentitiesReportingInterval, n.reportPeerIdentities(logger))

	async.Interval(n.ctx, topicsReportingInterval, n.reportTopics(logger))

	if err := n.subscribeToSubnets(logger); err != nil {
		return err
	}

	// Create & start ConcurrentSyncer.
	syncer := syncing.NewConcurrent(n.ctx, syncing.New(n), 16, syncing.DefaultTimeouts, nil)
	go syncer.Run(logger)
	n.syncer = syncer

	return nil
}

func (n *p2pNetwork) peersBalancing(logger *zap.Logger) func() {
	return func() {
		allPeers := n.host.Network().Peers()
		currentCount := len(allPeers)
		if currentCount < n.cfg.MaxPeers {
			_ = n.idx.GetSubnetsStats() // trigger metrics update
			return
		}
		ctx, cancel := context.WithTimeout(n.ctx, connManagerGCTimeout)
		defer cancel()

		connMgr := peers.NewConnManager(logger, n.libConnManager, n.idx)
		mySubnets := records.Subnets(n.subnets).Clone()
		connMgr.TagBestPeers(logger, n.cfg.MaxPeers-1, mySubnets, allPeers, n.cfg.TopicMaxPeers)
		connMgr.TrimPeers(ctx, logger, n.host.Network())
	}
}

// startDiscovery starts the required services
// it will try to bootstrap discovery service, and inject a connect function.
// the connect function checks if we can connect to the given peer and if so passing it to the backoff connector.
func (n *p2pNetwork) startDiscovery(logger *zap.Logger) {
	discoveredPeers := make(chan peer.AddrInfo, connectorQueueSize)
	go func() {
		ctx, cancel := context.WithCancel(n.ctx)
		defer cancel()
		n.backoffConnector.Connect(ctx, discoveredPeers)
	}()
	err := tasks.Retry(func() error {
		return n.disc.Bootstrap(logger, func(e discovery.PeerEvent) {
			if !n.idx.CanConnect(e.AddrInfo.ID) {
				return
			}
			select {
			case discoveredPeers <- e.AddrInfo:
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
// NOTE: it won't subscribe to the subnets (use subscribeToSubnets for that)
func (n *p2pNetwork) UpdateSubnets(logger *zap.Logger) {
	logger = logger.Named(logging.NameP2PNetwork)

	visited := make(map[int]bool)
	last := make([]byte, len(n.subnets))
	if len(n.subnets) > 0 {
		copy(last, n.subnets)
	}
	newSubnets := make([]byte, n.fork.Subnets())
	n.activeValidators.Range(func(pkHex string, status validatorStatus) bool {
		if status == validatorStatusInactive {
			return true
		}
		subnet := n.fork.ValidatorSubnet(pkHex)
		if _, ok := visited[subnet]; ok {
			return true
		}
		newSubnets[subnet] = byte(1)
		return true
	})
	subnetsToAdd := make([]int, 0)
	if !bytes.Equal(newSubnets, last) { // have changes
		n.subnets = newSubnets
		for i, b := range newSubnets {
			if b == byte(1) {
				subnetsToAdd = append(subnetsToAdd, i)
			}
		}
	}

	if len(subnetsToAdd) == 0 {
		return
	}

	self := n.idx.Self()
	self.Metadata.Subnets = records.Subnets(n.subnets).String()
	n.idx.UpdateSelfRecord(self)

	err := n.disc.RegisterSubnets(logger.Named(logging.NameDiscoveryService), subnetsToAdd...)
	if err != nil {
		logger.Warn("could not register subnets", zap.Error(err))
		return
	}
	allSubs, _ := records.Subnets{}.FromString(records.AllSubnets)
	subnetsList := records.SharedSubnets(allSubs, n.subnets, 0)
	logger.Debug("updated subnets (node-info)", zap.Any("subnets", subnetsList))
}

// getMaxPeers returns max peers of the given topic.
func (n *p2pNetwork) getMaxPeers(topic string) int {
	if len(topic) == 0 {
		return n.cfg.MaxPeers
	}
	return n.cfg.TopicMaxPeers
}
