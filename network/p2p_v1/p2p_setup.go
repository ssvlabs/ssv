package p2pv1

import (
	"github.com/bloxapp/ssv/network/commons"
	"github.com/bloxapp/ssv/network/p2p_v1/discovery"
	"github.com/bloxapp/ssv/network/p2p_v1/peers"
	"github.com/bloxapp/ssv/network/p2p_v1/streams"
	"github.com/bloxapp/ssv/network/p2p_v1/topics"
	"github.com/libp2p/go-libp2p"
	"github.com/libp2p/go-libp2p/p2p/protocol/identify"
	"github.com/pkg/errors"
	"go.uber.org/zap"
	"time"
)

const (
	defaultReqTimeout = 10 * time.Second
)

// Setup is used to setup the network
func (n *p2pNetwork) Setup() error {
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
	n.streamCtrl = streams.NewStreamController(n.ctx, n.logger, n.host, n.cfg.Fork, n.cfg.RequestTimeout)
	n.logger.Debug("stream controller is ready")
	return nil
}

func (n *p2pNetwork) setupPeerServices() error {
	self := peers.NewIdentity(n.host.ID().String(), "", "", make(map[string]string))
	n.idx = peers.NewPeersIndex(n.cfg.Logger, n.host.Network(), self, func() int {
		return n.cfg.MaxPeers
	}, 10*time.Minute)
	n.logger.Debug("peers index is ready")

	ids, err := identify.NewIDService(n.host, identify.UserAgent(userAgent(n.cfg.UserAgent)))
	if err != nil {
		return errors.Wrap(err, "failed to create ID service")
	}

	handshaker := peers.NewHandshaker(n.ctx, n.cfg.Logger, n.streamCtrl, n.idx, ids)
	n.host.SetStreamHandler(peers.HandshakeProtocol, handshaker.Handler())
	n.logger.Debug("handshaker is ready")

	connHandler := peers.HandleConnections(n.ctx, n.cfg.Logger, handshaker)
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
			Logger:     n.cfg.Logger,
		}
	}
	discOpts := discovery.Options{
		Logger:     n.cfg.Logger,
		Host:       n.host,
		DiscV5Opts: discV5Opts,
		ConnIndex:  n.idx,
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
	psBundle, err := topics.NewPubsub(n.ctx, &topics.PububConfig{
		Logger:      n.cfg.Logger,
		Host:        n.host,
		TraceLog:    n.cfg.PubSubTrace,
		StaticPeers: nil,
		UseMsgID:    true,
		MsgValidatorFactory: func(s string) topics.MsgValidatorFunc {
			logger := n.cfg.Logger.With(zap.String("who", "MsgValidator"))
			return topics.NewSSVMsgValidator(logger, n.cfg.Fork, n.host.ID())
		},
		MsgHandler: n.handlePubsubMessages,
	})
	if err != nil {
		return errors.Wrap(err, "could not setup pubsub")
	}
	n.topicsCtrl = psBundle.TopicsCtrl
	n.msgResolver = psBundle.Resolver
	n.logger.Debug("topics controller is ready")
	return nil
}

func (n *p2pNetwork) setupSyncHandlers() error {
	// TODO: complete
	return nil
}
