package p2pv1

import (
	"context"
	"fmt"
	"strings"
	"sync/atomic"
	"time"

	"github.com/cornelk/hashmap"
	"github.com/libp2p/go-libp2p/core/connmgr"
	connmgrcore "github.com/libp2p/go-libp2p/core/connmgr"
	"github.com/libp2p/go-libp2p/core/host"
	"github.com/libp2p/go-libp2p/core/peer"
	libp2pdiscbackoff "github.com/libp2p/go-libp2p/p2p/discovery/backoff"
	ma "github.com/multiformats/go-multiaddr"
	"go.uber.org/zap"

	"github.com/bloxapp/ssv/logging"
	"github.com/bloxapp/ssv/logging/fields"
	"github.com/bloxapp/ssv/message/validation"
	"github.com/bloxapp/ssv/network"
	"github.com/bloxapp/ssv/network/commons"
	"github.com/bloxapp/ssv/network/discovery"
	"github.com/bloxapp/ssv/network/peers"
	"github.com/bloxapp/ssv/network/peers/connections"
	"github.com/bloxapp/ssv/network/records"
	"github.com/bloxapp/ssv/network/streams"
	"github.com/bloxapp/ssv/network/topics"
	operatordatastore "github.com/bloxapp/ssv/operator/datastore"
	"github.com/bloxapp/ssv/operator/keys"
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
	connManagerGCInterval           = 3 * time.Minute
	connManagerGCTimeout            = time.Minute
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
	metrics      Metrics

	state int32

	activeValidators *hashmap.Map[string, validatorStatus]

	backoffConnector *libp2pdiscbackoff.BackoffConnector
	subnets          []byte
	libConnManager   connmgrcore.ConnManager

	nodeStorage             operatorstorage.Storage
	operatorPKHashToPKCache *hashmap.Map[string, []byte] // used for metrics
	operatorSigner          keys.OperatorSigner
	operatorDataStore       operatordatastore.OperatorDataStore
}

// New creates a new p2p network
func New(logger *zap.Logger, cfg *Config, mr Metrics) (network.P2PNetwork, error) {
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
		activeValidators:        hashmap.New[string, validatorStatus](),
		nodeStorage:             cfg.NodeStorage,
		operatorPKHashToPKCache: hashmap.New[string, []byte](),
		operatorSigner:          cfg.OperatorSigner,
		operatorDataStore:       cfg.OperatorDataStore,
		metrics:                 mr,
	}
	if err := n.parseTrustedPeers(); err != nil {
		return nil, err
	}
	return n, nil
}

func (n *p2pNetwork) parseTrustedPeers() error {
	// Group addresses by peer ID.
	trustedPeers := map[peer.ID][]ma.Multiaddr{}
	if len(n.cfg.TrustedPeers) > 0 {
		for _, mas := range n.cfg.TrustedPeers {
			for _, ma := range strings.Split(mas, ",") {
				addrInfo, err := peer.AddrInfoFromString(ma)
				if err != nil {
					return fmt.Errorf("could not parse trusted peer: %w", err)
				}
				trustedPeers[addrInfo.ID] = append(trustedPeers[addrInfo.ID], addrInfo.Addrs...)
			}
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

// Start starts the discovery service, garbage collector (peer index), and reporting.
func (n *p2pNetwork) Start(logger *zap.Logger) error {
	logger = logger.Named(logging.NameP2PNetwork)

	if atomic.SwapInt32(&n.state, stateReady) == stateReady {
		// return errors.New("could not setup network: in ready state")
		return nil
	}

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

	ma, err := peer.AddrInfoToP2pAddrs(&peer.AddrInfo{
		ID:    n.host.ID(),
		Addrs: n.host.Addrs(),
	})
	if err != nil {
		logger.Fatal("could not get my address", zap.Error(err))
	}
	maStrs := make([]string, len(ma))
	for i, ima := range ma {
		maStrs[i] = ima.String()
	}
	logger.Info("starting p2p",
		zap.String("my_address", strings.Join(maStrs, ",")),
		zap.Int("trusted_peers", len(n.trustedPeers)),
	)

	go n.startDiscovery(logger, connector)

	async.Interval(n.ctx, connManagerGCInterval, n.peersBalancing(logger))
	// don't report metrics in tests
	if n.cfg.Metrics != nil {
		async.Interval(n.ctx, peersReportingInterval, n.reportAllPeers(logger))

		async.Interval(n.ctx, peerIdentitiesReportingInterval, n.reportPeerIdentities(logger))

		async.Interval(n.ctx, topicsReportingInterval, n.reportTopics(logger))
	}

	if err := n.subscribeToSubnets(logger); err != nil {
		return err
	}

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
func (n *p2pNetwork) startDiscovery(logger *zap.Logger, connector chan peer.AddrInfo) {
	err := tasks.Retry(func() error {
		return n.disc.Bootstrap(logger, func(e discovery.PeerEvent) {
			if !n.idx.CanConnect(e.AddrInfo.ID) {
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
// NOTE: it won't subscribe to the subnets (use subscribeToSubnets for that)
func (n *p2pNetwork) UpdateSubnets(logger *zap.Logger) {
	// TODO: this is a temporary fix to update subnets when validators are added/removed,
	// there is a pending PR to replace this: https://github.com/bloxapp/ssv/pull/990
	logger = logger.Named(logging.NameP2PNetwork)
	ticker := time.NewTicker(time.Second)
	registeredSubnets := make([]byte, commons.Subnets())
	defer ticker.Stop()

	// Run immediately and then every second.
	for ; true; <-ticker.C {
		start := time.Now()

		// Compute the new subnets according to the active validators.
		newSubnets := make([]byte, commons.Subnets())
		copy(newSubnets, n.subnets)
		n.activeValidators.Range(func(pkHex string, status validatorStatus) bool {
			subnet := commons.ValidatorSubnet(pkHex)
			newSubnets[subnet] = byte(1)
			return true
		})
		n.subnets = newSubnets

		// Compute the not yet registered subnets.
		unregisteredSubnets := make([]int, 0)
		for subnet, active := range newSubnets {
			if active == byte(1) && registeredSubnets[subnet] == byte(0) {
				unregisteredSubnets = append(unregisteredSubnets, subnet)
			}
		}
		registeredSubnets = newSubnets

		if len(unregisteredSubnets) == 0 {
			continue
		}

		self := n.idx.Self()
		self.Metadata.Subnets = records.Subnets(n.subnets).String()
		n.idx.UpdateSelfRecord(self)

		err := n.disc.RegisterSubnets(logger.Named(logging.NameDiscoveryService), unregisteredSubnets...)
		if err != nil {
			logger.Warn("could not register subnets", zap.Error(err))
			continue
		}
		allSubs, _ := records.Subnets{}.FromString(records.AllSubnets)
		subnetsList := records.SharedSubnets(allSubs, n.subnets, 0)
		logger.Debug("updated subnets",
			zap.Any("added", unregisteredSubnets),
			zap.Any("subnets", subnetsList),
			zap.Int("total_subnets", len(subnetsList)),
			zap.Duration("took", time.Since(start)),
		)
	}
}

// getMaxPeers returns max peers of the given topic.
func (n *p2pNetwork) getMaxPeers(topic string) int {
	if len(topic) == 0 {
		return n.cfg.MaxPeers
	}
	return n.cfg.TopicMaxPeers
}
