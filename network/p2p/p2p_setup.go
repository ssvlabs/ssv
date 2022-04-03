package p2pv1

import (
	"github.com/bloxapp/ssv/network/commons"
	"github.com/bloxapp/ssv/network/discovery"
	"github.com/bloxapp/ssv/network/peers"
	"github.com/bloxapp/ssv/network/streams"
	"github.com/bloxapp/ssv/network/topics"
	forksprotocol "github.com/bloxapp/ssv/protocol/forks"
	"github.com/libp2p/go-libp2p"
	"github.com/libp2p/go-libp2p/p2p/protocol/identify"
	"github.com/pkg/errors"
	"github.com/prysmaticlabs/prysm/async"
	"go.uber.org/zap"
	"sync/atomic"
	"time"
)

const (
	defaultReqTimeout = 10 * time.Second
)

// Setup is used to setup the network
func (n *p2pNetwork) Setup() error {
	if atomic.SwapInt32(&n.state, stateInitializing) == stateReady {
		return errors.New("could not setup network: in ready state")
	}

	n.logger.Info("configuring p2p network service")

	n.initCfg()

	err := n.SetupHost()
	if err != nil {
		return err
	}
	n.logger.Debug("p2p host was configured", zap.String("peer", n.host.ID().String()))

	err = n.SetupServices()
	if err != nil {
		return err
	}
	n.logger.Debug("p2p services were configured", zap.String("peer", n.host.ID().String()))

	return nil
}

func (n *p2pNetwork) initCfg() {
	if n.cfg.RequestTimeout == 0 {
		n.cfg.RequestTimeout = defaultReqTimeout
	}
	if len(n.cfg.UserAgent) == 0 {
		n.cfg.UserAgent = userAgent(n.cfg.UserAgent)
	}
}

// SetupHost configures a libp2p host
func (n *p2pNetwork) SetupHost() error {
	opts, err := n.cfg.Libp2pOptions()
	if err != nil {
		return errors.Wrap(err, "could not create libp2p options")
	}
	host, err := libp2p.New(n.ctx, opts...)
	if err != nil {
		return errors.Wrap(err, "failed to create p2p host")
	}
	n.host = host
	return nil
}

// SetupServices configures the required services
func (n *p2pNetwork) SetupServices() error {
	if err := n.setupStreamCtrl(); err != nil {
		return errors.Wrap(err, "could not setup stream controller")
	}
	if err := n.setupPeerServices(); err != nil {
		return errors.Wrap(err, "could not setup peer services")
	}
	if err := n.setupDiscovery(); err != nil {
		return errors.Wrap(err, "could not setup discovery service")
	}
	if err := n.setupPubsub(); err != nil {
		return errors.Wrap(err, "could not setup topic controller")
	}
	if err := n.setupSyncHandlers(); err != nil {
		return errors.Wrap(err, "could not setup sync handlers")
	}

	return nil
}

func (n *p2pNetwork) setupStreamCtrl() error {
	n.streamCtrl = streams.NewStreamController(n.ctx, n.logger, n.host, n.fork, n.cfg.RequestTimeout)
	n.logger.Debug("stream controller is ready")
	return nil
}

func (n *p2pNetwork) setupPeerServices() error {
	self := peers.NewIdentity(n.host.ID().String(), n.cfg.OperatorID, string(n.cfg.ForkVersion), make(map[string]string))
	n.idx = peers.NewPeersIndex(n.logger, n.host.Network(), self, func() int {
		return n.cfg.MaxPeers
	}, 10*time.Minute)
	n.logger.Debug("peers index is ready", zap.String("forkVersion", string(n.cfg.ForkVersion)))

	ids, err := identify.NewIDService(n.host, identify.UserAgent(userAgent(n.cfg.UserAgent)))
	if err != nil {
		return errors.Wrap(err, "failed to create ID service")
	}

	filters := make([]peers.HandshakeFilter, 0)
	// v0 was before we checked forks, therefore asking if we are above v0
	if n.cfg.ForkVersion != forksprotocol.V0ForkVersion {
		filters = append(filters, peers.ForkVersionFilter(n.cfg.ForkVersion))
	}
	handshaker := peers.NewHandshaker(n.ctx, n.logger, n.streamCtrl, n.idx, ids, filters...)
	n.host.SetStreamHandler(peers.HandshakeProtocol, handshaker.Handler())
	n.logger.Debug("handshaker is ready")

	connHandler := peers.HandleConnections(n.ctx, n.logger, handshaker)
	n.host.Network().Notify(connHandler)
	n.logger.Debug("connection handler is ready")
	return nil
}

func (n *p2pNetwork) setupDiscovery() error {
	ipAddr, err := commons.IPAddr()
	if err != nil {
		return errors.Wrap(err, "could not get ip addr")
	}
	var discV5Opts *discovery.DiscV5Options
	if len(n.cfg.Bootnodes) > 0 { // otherwise, we are in local scenario
		discV5Opts = &discovery.DiscV5Options{
			IP:         ipAddr.String(),
			BindIP:     "", // net.IPv4zero.String()
			Port:       n.cfg.UDPPort,
			TCPPort:    n.cfg.TCPPort,
			NetworkKey: n.cfg.NetworkPrivateKey,
			Bootnodes:  n.cfg.TransformBootnodes(),
			Logger:     n.logger,
		}
	}
	discOpts := discovery.Options{
		Logger:      n.logger,
		Host:        n.host,
		DiscV5Opts:  discV5Opts,
		ConnIndex:   n.idx,
		HostAddress: n.cfg.HostAddress,
		HostDNS:     n.cfg.HostDNS,
		//ForkVersion: n.cfg.ForkVersion,
	}
	disc, err := discovery.NewService(n.ctx, discOpts)
	if err != nil {
		return err
	}
	n.disc = disc

	n.logger.Debug("discovery is ready")

	return nil
}

func (n *p2pNetwork) setupPubsub() error {
	cfg := &topics.PububConfig{
		Logger:      n.logger,
		Host:        n.host,
		TraceLog:    n.cfg.PubSubTrace,
		StaticPeers: nil,
		MsgValidatorFactory: func(s string) topics.MsgValidatorFunc {
			logger := n.logger.With(zap.String("who", "MsgValidator"))
			return topics.NewSSVMsgValidator(logger, n.fork, n.host.ID())
		},
		MsgHandler: n.handlePubsubMessages,
		ScoreIndex: n.idx,
	}

	if !n.cfg.PubSubScoring {
		cfg.ScoreIndex = nil
	}

	if n.fork.MsgID() != nil {
		midHandler := topics.NewMsgIDHandler(n.logger.With(zap.String("who", "msgIDHandler")),
			n.fork, time.Minute*2)
		n.msgResolver = midHandler
		cfg.MsgIDHandler = topics.NewMsgIDHandler(n.logger, n.fork, time.Minute*2)
		// run GC every 3 minutes to clear old messages
		async.RunEvery(n.ctx, time.Minute*3, midHandler.GC)
	} else {

	}
	_, tc, err := topics.NewPubsub(n.ctx, cfg)
	if err != nil {
		return errors.Wrap(err, "could not setup pubsub")
	}
	n.topicsCtrl = tc
	n.logger.Debug("topics controller is ready")
	return nil
}

func (n *p2pNetwork) setupSyncHandlers() error {
	// TODO: complete
	return nil
}
