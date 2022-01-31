package p2p

import (
	"context"
	"crypto/ecdsa"
	"crypto/rsa"
	"github.com/bloxapp/ssv/network/commons/listeners"
	"github.com/bloxapp/ssv/network/forks"
	"github.com/bloxapp/ssv/network/p2p/streams"
	"github.com/bloxapp/ssv/utils/commons"
	"github.com/bloxapp/ssv/utils/rsaencryption"
	"github.com/prysmaticlabs/prysm/async"
	"sync"
	"time"

	"github.com/libp2p/go-libp2p"
	p2pHost "github.com/libp2p/go-libp2p-core/host"
	pubsub "github.com/libp2p/go-libp2p-pubsub"
	"github.com/libp2p/go-libp2p/p2p/protocol/identify"
	"github.com/pkg/errors"
	"go.uber.org/zap"

	"github.com/bloxapp/ssv/network"
)

const (
	topicPrefix = "bloxstaking.ssv"

	// minPeers is the min value for peers limit
	minPeers = 10

	//baseSyncStream           = "/sync/"
	//highestDecidedStream     = baseSyncStream + "highest_decided"
	//decidedByRangeStream     = baseSyncStream + "decided_by_range"
	//lastChangeRoundMsgStream = baseSyncStream + "last_change_round"
	legacyMsgStream = "/sync/0.0.1"
)

// p2pNetwork implements network.Network interface using P2P
type p2pNetwork struct {
	ctx             context.Context
	cfg             *Config
	dv5Listener     discv5Listener
	listeners       listeners.Container
	logger          *zap.Logger
	privKey         *ecdsa.PrivateKey
	host            p2pHost.Host
	pubsub          *pubsub.PubSub
	peersIndex      PeersIndex
	operatorPrivKey *rsa.PrivateKey
	fork            forks.Fork

	streamCtrl streams.StreamController

	psSubs       map[string]context.CancelFunc
	psTopicsLock *sync.RWMutex

	useMainTopic  bool
	reportLastMsg bool
	nodeType      NodeType

	lookupOperator LookupOperatorHandler
	peersLimit     int
}

// LookupOperatorHandler is a function that checks if the given operator
// has some shared validator with the running operator
type LookupOperatorHandler func(string) bool

// UseLookupOperatorHandler enables to inject some lookup handler
func UseLookupOperatorHandler(n network.Network, fn LookupOperatorHandler) {
	if net, ok := n.(*p2pNetwork); ok {
		net.lookupOperator = fn
	}
}

// New is the constructor of p2pNetworker
func New(ctx context.Context, logger *zap.Logger, cfg *Config) (network.Network, error) {
	// init empty topics map
	cfg.Topics = make(map[string]*pubsub.Topic)

	logger = logger.With(zap.String("component", "p2p"))

	// ensuring min peers
	if cfg.MaxPeers < minPeers {
		cfg.MaxPeers = minPeers
	}

	n := &p2pNetwork{
		ctx:             ctx,
		cfg:             cfg,
		listeners:       listeners.NewListenersContainer(ctx, logger),
		logger:          logger,
		operatorPrivKey: cfg.OperatorPrivateKey,
		privKey:         cfg.NetworkPrivateKey,
		psSubs:          make(map[string]context.CancelFunc),
		psTopicsLock:    &sync.RWMutex{},
		reportLastMsg:   cfg.ReportLastMsg,
		fork:            cfg.Fork,
		nodeType:        cfg.NodeType,
		peersLimit:      cfg.MaxPeers,
		lookupOperator: func(s string) bool {
			return true
		},
	}

	n.cfg.BootnodesENRs = filterInvalidENRs(n.logger, TransformEnr(n.cfg.Enr))
	if len(n.cfg.BootnodesENRs) == 0 {
		n.logger.Warn("missing valid bootnode ENR")
	}

	// create libp2p host
	opts, err := n.buildOptions(cfg)
	if err != nil {
		logger.Fatal("could not build libp2p options", zap.Error(err))
	}
	host, err := libp2p.New(ctx, opts...)
	if err != nil {
		return nil, errors.Wrap(err, "failed to create p2p host")
	}
	n.host = host
	n.streamCtrl = streams.NewStreamController(ctx, logger, host, cfg.Fork, cfg.RequestTimeout)
	n.cfg.HostID = host.ID()
	n.logger = logger.With(zap.String("id", n.cfg.HostID.String()))
	n.logger.Info("listening on port", zap.String("addr", n.host.Addrs()[0].String()))

	// create ID service
	ua := n.getUserAgent()
	ids, err := identify.NewIDService(host, identify.UserAgent(ua))
	if err != nil {
		return nil, errors.Wrap(err, "failed to create ID service")
	}
	n.logger.Info("libp2p User Agent", zap.String("value", ua))
	n.peersIndex = NewPeersIndex(n.logger, n.host, ids)

	// setting up connection handler and the corresponding filters
	filters := []ConnectionFilter{
		n.filterNonSsvNodes,
		//n.filterOldVersion,
	}
	if cfg.DiscoveryType != discoveryTypeMdns {
		filters = append(filters, n.filterIrrelevant)
	}
	notifyHandler := n.handleConnections(filters...)
	n.host.Network().Notify(notifyHandler)

	ps, err := n.newGossipPubsub(cfg)
	if err != nil {
		n.logger.Error("failed to start pubsub", zap.Error(err))
		return nil, errors.Wrap(err, "failed to start pubsub")
	}
	n.pubsub = ps

	if err := n.setupDiscovery(); err != nil {
		return nil, errors.Wrap(err, "failed to setup discovery")
	}
	if err := n.startDiscovery(); err != nil {
		return nil, errors.Wrap(err, "failed to start discovery")
	}

	n.setStreamHandlers()

	n.watchPeers()

	return n, nil
}

func (n *p2pNetwork) setStreamHandlers() {
	n.setLegacyStreamHandler()
	//n.setHighestDecidedStreamHandler()
	//n.setDecidedByRangeStreamHandler()
	//n.setLastChangeRoundStreamHandler()
}

func (n *p2pNetwork) watchPeers() {
	async.RunEvery(n.ctx, 5*time.Minute, func() {
		n.peersIndex.Run()
	})

	async.RunEvery(n.ctx, 1*time.Minute, func() {
		go reportAllPeers(n)

		// topics peers
		n.psTopicsLock.RLock()
		defer n.psTopicsLock.RUnlock()
		for name, topic := range n.cfg.Topics {
			reportTopicPeers(n, name, topic)
		}
	})
}

func (n *p2pNetwork) MaxBatch() uint64 {
	return n.cfg.MaxBatchResponse
}

// NotifyOperatorID updates the network regarding new operators joining the network
func (n *p2pNetwork) NotifyOperatorID(oid string) {
	n.trace("notified on operator id", zap.String("operatorID", oid))
	n.peersIndex.EvictPruned(oid)
}

// getUserAgent returns ua built upon:
// - node version
// - node type ('operator' | 'exporter')
// - operator ID
func (n *p2pNetwork) getUserAgent() string {
	ua, err := GenerateUserAgent(n.operatorPrivKey, n.nodeType)
	if err != nil {
		n.logger.Error("could not generate user agent", zap.Error(err))
		bd := commons.GetBuildData()
		ua = NewUserAgent(bd)
	}
	return string(ua)
}

func (n *p2pNetwork) getOperatorPubKey() (string, error) {
	if n.operatorPrivKey != nil {
		operatorPubKey, err := rsaencryption.ExtractPublicKey(n.operatorPrivKey)
		if err != nil || len(operatorPubKey) == 0 {
			n.logger.Error("could not extract operator public key", zap.Error(err))
			return "", errors.Wrap(err, "could not extract operator public key")
		}
		return operatorPubKey, nil
	}
	return "", nil
}

func (n *p2pNetwork) trace(msg string, fields ...zap.Field) {
	if n.cfg != nil && n.cfg.NetworkTrace {
		n.logger.Debug(msg, fields...)
	}
}
