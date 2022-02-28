package p2pv1

import (
	"context"
	"github.com/bloxapp/ssv/network/p2p_v1/discovery"
	"github.com/bloxapp/ssv/network/p2p_v1/peers"
	"github.com/bloxapp/ssv/network/p2p_v1/streams"
	"github.com/bloxapp/ssv/network/p2p_v1/topics"
	"github.com/bloxapp/ssv/utils/tasks"
	"github.com/libp2p/go-libp2p-core/host"
	"github.com/libp2p/go-libp2p-core/peer"
	"github.com/libp2p/go-libp2p/p2p/protocol/identify"
	"github.com/pkg/errors"
	"github.com/prysmaticlabs/prysm/async"
	"go.uber.org/zap"
	"io"
	"time"
)

// P2PNetwork is the interface for p2p network
// TODO: extend network.Network
type P2PNetwork interface {
	Start() error
	Setup() error

	io.Closer
}

type p2pNetwork struct {
	ctx        context.Context
	cancel     context.CancelFunc
	logger     *zap.Logger
	cfg        *Config
	host       host.Host
	streamCtrl streams.StreamController
	ids        *identify.IDService
	idx        peers.Index
	disc       discovery.Service
	topicsCtrl topics.Controller
}

// New creates a new p2p network
func New(pctx context.Context, cfg *Config) P2PNetwork {
	ctx, cancel := context.WithCancel(pctx)
	p := &p2pNetwork{
		ctx:    ctx,
		cancel: cancel,
		logger: cfg.Logger.With(zap.String("who", "p2pNetwork")),
		cfg:    cfg,
	}
	return p
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
		go reportAllPeers(n)
		reportTopics(n)
	})

	return nil
}

// Start starts the required services
func (n *p2pNetwork) startDiscovery() {
	err := tasks.Retry(func() error {
		return n.disc.Bootstrap(func(e discovery.PeerEvent) {
			ctx, cancel := context.WithTimeout(n.ctx, n.cfg.RequestTimeout)
			defer cancel()
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

func (n *p2pNetwork) Connect(info *peer.AddrInfo) error {
	if info == nil {
		return errors.New("empty info")
	}
	ctx, cancel := context.WithTimeout(n.ctx, n.cfg.RequestTimeout)
	defer cancel()
	return n.host.Connect(ctx, *info)
}
