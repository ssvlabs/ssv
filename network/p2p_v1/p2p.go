package p2pv1

import (
	"context"
	"github.com/bloxapp/ssv/network"
	"github.com/bloxapp/ssv/network/p2p_v1/discovery"
	"github.com/bloxapp/ssv/network/p2p_v1/peers"
	"github.com/bloxapp/ssv/network/p2p_v1/streams"
	"github.com/bloxapp/ssv/network/p2p_v1/topics"
	"github.com/bloxapp/ssv/utils/tasks"
	"github.com/libp2p/go-libp2p-core/host"
	libp2pnetwork "github.com/libp2p/go-libp2p-core/network"
	"github.com/libp2p/go-libp2p/p2p/protocol/identify"
	"github.com/prysmaticlabs/prysm/async"
	"go.uber.org/zap"
	"time"
)

// p2pNetwork implements network.V1
type p2pNetwork struct {
	ctx         context.Context
	cancel      context.CancelFunc
	logger      *zap.Logger
	cfg         *Config
	host        host.Host
	streamCtrl  streams.StreamController
	ids         *identify.IDService
	idx         peers.Index
	disc        discovery.Service
	topicsCtrl  topics.Controller
	msgRouter   network.MessageRouter
	msgResolver topics.MsgPeersResolver
}

// New creates a new p2p network
func New(pctx context.Context, cfg *Config) network.V1 {
	ctx, cancel := context.WithCancel(pctx)
	return &p2pNetwork{
		ctx:       ctx,
		cancel:    cancel,
		logger:    cfg.Logger.With(zap.String("who", "p2pNetwork")),
		cfg:       cfg,
		msgRouter: cfg.Router,
	}
}

// Close implements io.Closer
func (n *p2pNetwork) Close() error {
	n.cancel()
	if err := n.idx.Close(); err != nil {
		n.logger.Error("could not close index", zap.Error(err))
	}
	return n.host.Close()
}

// Start starts the required services
func (n *p2pNetwork) Start() error {
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
			cntd := n.idx.Connectedness(e.AddrInfo.ID)
			switch cntd {
			case libp2pnetwork.Connected:
				fallthrough
			case libp2pnetwork.CannotConnect: // recently failed to connect
				return
			default:
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
