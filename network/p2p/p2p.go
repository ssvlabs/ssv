package p2pv1

import (
	"context"
	"github.com/bloxapp/ssv/network"
	"github.com/bloxapp/ssv/network/discovery"
	"github.com/bloxapp/ssv/network/forks"
	forksfactory "github.com/bloxapp/ssv/network/forks/factory"
	"github.com/bloxapp/ssv/network/peers"
	"github.com/bloxapp/ssv/network/peers/connections"
	"github.com/bloxapp/ssv/network/streams"
	"github.com/bloxapp/ssv/network/topics"
	"github.com/bloxapp/ssv/utils/async"
	"github.com/bloxapp/ssv/utils/tasks"
	connmgrcore "github.com/libp2p/go-libp2p-core/connmgr"
	"github.com/libp2p/go-libp2p-core/host"
	"github.com/libp2p/go-libp2p-core/peer"
	libp2pdisc "github.com/libp2p/go-libp2p-discovery"
	"github.com/pkg/errors"
	"go.uber.org/zap"
	"sync"
	"sync/atomic"
	"time"
)

// network states
const (
	stateInitializing int32 = 0
	stateForking      int32 = 1
	stateClosing      int32 = 1
	stateClosed       int32 = 2
	stateReady        int32 = 10
)

const (
	connManagerGCInterval = 5 * time.Minute
	peerIndexGCInterval   = 15 * time.Minute
	reportingInterval     = 30 * time.Second
)

// p2pNetwork implements network.P2PNetwork
type p2pNetwork struct {
	parentCtx context.Context
	ctx       context.Context
	cancel    context.CancelFunc

	logger *zap.Logger
	fork   forks.Fork
	cfg    *Config

	host        host.Host
	streamCtrl  streams.StreamController
	idx         peers.Index
	disc        discovery.Service
	topicsCtrl  topics.Controller
	msgRouter   network.MessageRouter
	msgResolver topics.MsgPeersResolver
	connHandler connections.ConnHandler

	state int32

	activeValidatorsLock *sync.Mutex
	activeValidators     map[string]int32

	backoffConnector *libp2pdisc.BackoffConnector
	subnets          []byte
	connManager      connmgrcore.ConnManager
}

// New creates a new p2p network
func New(pctx context.Context, cfg *Config) network.P2PNetwork {
	ctx, cancel := context.WithCancel(pctx)

	return &p2pNetwork{
		parentCtx:            pctx,
		ctx:                  ctx,
		cancel:               cancel,
		logger:               cfg.Logger.With(zap.String("who", "p2pNetwork")),
		fork:                 forksfactory.NewFork(cfg.ForkVersion),
		cfg:                  cfg,
		msgRouter:            cfg.Router,
		state:                stateClosed,
		activeValidators:     make(map[string]int32),
		activeValidatorsLock: &sync.Mutex{},
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
	if err := n.connManager.Close(); err != nil {
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

// Start starts the discovery service, garbage collector (peer index), and reporting.
func (n *p2pNetwork) Start() error {
	if atomic.SwapInt32(&n.state, stateReady) == stateReady {
		//return errors.New("could not setup network: in ready state")
		return nil
	}

	n.logger.Info("starting p2p network service")

	go n.startDiscovery()

	async.Interval(n.ctx, connManagerGCInterval, func() {
		ctx, cancel := context.WithCancel(n.ctx)
		defer cancel()
		// tag the best 80% nodes and trim
		n.tagBestPeers((n.cfg.MaxPeers / 10) * 8)
		n.connManager.TrimOpenConns(ctx)
	})

	async.Interval(n.ctx, peerIndexGCInterval, func() {
		n.idx.GC()
	})

	async.Interval(n.ctx, reportingInterval, func() {
		go n.reportAllPeers()
		n.reportTopics()
	})

	async.Interval(n.ctx, reportingInterval/15, func() {
		n.reportSubnetsStats()
	})

	if err := n.registerInitialTopics(); err != nil {
		return err
	}

	return nil
}

func (n *p2pNetwork) registerInitialTopics() error {
	decidedTopic := n.fork.DecidedTopic()
	if len(decidedTopic) == 0 {
		return nil
	}
	// start listening to decided topic
	err := tasks.RetryWithContext(n.ctx, func() error {
		if err := n.topicsCtrl.Subscribe(decidedTopic); err != nil {
			n.logger.Warn("could not register to decided topic", zap.Error(err))
			return err
		}
		return nil
	}, 3)
	if err != nil {
		return errors.Wrap(err, "could not register to decided topic")
	}
	_ = n.subscribeToSubnets()

	return nil
}

// startDiscovery starts the required services
// it will try to bootstrap discovery service, and inject a connect function.
// the connect function checks if we can connect to the given peer and if so passing it to the backoff connector.
func (n *p2pNetwork) startDiscovery() {
	discoveredPeers := make(chan peer.AddrInfo, connectorQueueSize)
	go func() {
		ctx, cancel := context.WithCancel(n.ctx)
		defer cancel()
		n.backoffConnector.Connect(ctx, discoveredPeers)
	}()
	err := tasks.Retry(func() error {
		return n.disc.Bootstrap(func(e discovery.PeerEvent) {
			if !n.idx.CanConnect(e.AddrInfo.ID) {
				return
			}
			select {
			case discoveredPeers <- e.AddrInfo:
			default:
				n.logger.Warn("connector queue is full, skipping new peer", zap.String("peerID", e.AddrInfo.ID.String()))
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
