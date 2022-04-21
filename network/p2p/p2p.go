package p2pv1

import (
	"context"
	"github.com/bloxapp/ssv/network"
	"github.com/bloxapp/ssv/network/discovery"
	"github.com/bloxapp/ssv/network/forks"
	forksfactory "github.com/bloxapp/ssv/network/forks/factory"
	"github.com/bloxapp/ssv/network/peers"
	"github.com/bloxapp/ssv/network/streams"
	"github.com/bloxapp/ssv/network/topics"
	"github.com/bloxapp/ssv/utils/tasks"
	"github.com/libp2p/go-libp2p-core/host"
	"github.com/pkg/errors"
	"github.com/prysmaticlabs/prysm/async"
	"go.uber.org/zap"
	"sync"
	"sync/atomic"
	"time"
)

const (
	stateInitializing int32 = 0
	stateForking      int32 = 1
	stateClosing      int32 = 1
	stateClosed       int32 = 2
	stateReady        int32 = 10
)

var (
	// ErrNetworkIsNotReady is returned when trying to access the network instance before it's ready
	ErrNetworkIsNotReady = errors.New("network services are not ready")
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

	state int32

	activeValidatorsLock *sync.Mutex
	activeValidators     map[string]bool
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
		activeValidators:     make(map[string]bool),
		activeValidatorsLock: &sync.Mutex{},
	}
}

// Host implements HostProvider
func (n *p2pNetwork) Host() host.Host {
	return n.host
}

// Close implements io.Closer
func (n *p2pNetwork) Close() error {
	// exit if network was already closed or currently closing
	if current := atomic.SwapInt32(&n.state, stateClosing); current == stateClosed || current == stateClosing {
		return nil
	}
	defer atomic.StoreInt32(&n.state, stateClosed)
	n.cancel()
	if err := n.disc.Close(); err != nil {
		n.logger.Error("could not close discovery", zap.Error(err))
	}
	if err := n.idx.Close(); err != nil {
		n.logger.Error("could not close index", zap.Error(err))
	}
	return n.host.Close()
}

// Start starts the required services
func (n *p2pNetwork) Start() error {
	if atomic.SwapInt32(&n.state, stateReady) == stateReady {
		//return errors.New("could not setup network: in ready state")
		return nil
	}

	n.logger.Info("starting p2p network service")

	go n.startDiscovery()

	async.RunEvery(n.ctx, 15*time.Minute, func() {
		n.idx.GC()
	})

	async.RunEvery(n.ctx, 30*time.Second, func() {
		go n.reportAllPeers()
		n.reportTopics()
	})

	return nil
}

// startDiscovery starts the required services
// it will try to bootstrap discovery service, and inject a connect function
// which will ignore the peer if it is connected or recently failed to connect
func (n *p2pNetwork) startDiscovery() {
	err := tasks.Retry(func() error {
		return n.disc.Bootstrap(func(e discovery.PeerEvent) {
			ctx, cancel := context.WithTimeout(n.ctx, n.cfg.RequestTimeout)
			defer cancel()
			if !n.idx.CanConnect(e.AddrInfo.ID) {
				return
			}
			if err := n.host.Connect(ctx, e.AddrInfo); err != nil {
				n.logger.Warn("could not connect to peer", zap.Any("peer", e), zap.Error(err))
				return
			}
			n.logger.Debug("connected peer from discovery", zap.Any("peer", e))
		})
	}, 3)
	if err != nil {
		n.logger.Panic("could not setup discovery", zap.Error(err))
	}
}

func (n *p2pNetwork) isReady() bool {
	return atomic.LoadInt32(&n.state) == stateReady
}
