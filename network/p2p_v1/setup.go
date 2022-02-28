package p2pv1

import (
	"context"
	"github.com/bloxapp/ssv/network/commons"
	"github.com/bloxapp/ssv/network/p2p_v1/discovery"
	"github.com/bloxapp/ssv/network/p2p_v1/peers"
	"github.com/bloxapp/ssv/network/p2p_v1/streams"
	"github.com/libp2p/go-libp2p"
	"github.com/libp2p/go-libp2p-core/peer"
	"github.com/libp2p/go-libp2p/p2p/protocol/identify"
	"github.com/pkg/errors"
	"go.uber.org/zap"
	"time"
)

// Setup is used to setup the network
func (n *p2pNetwork) Setup() error {
	n.logger.Info("configuring p2p network service")

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
	n.streamCtrl = streams.NewStreamController(n.ctx, n.logger, n.host, n.cfg.Fork, n.cfg.RequestTimeout)
	n.logger.Debug("stream controller is ready")
	ids, err := identify.NewIDService(n.host, identify.UserAgent(userAgent()))
	if err != nil {
		return errors.Wrap(err, "failed to create ID service")
	}
	n.ids = ids

	self := peers.NewIdentity(n.host.ID().String(), "", "", make(map[string]string))
	n.idx = peers.NewPeersIndex(n.cfg.Logger, n.host.Network(), self, func() int {
		return n.cfg.MaxPeers
	}, 10*time.Minute)
	n.logger.Debug("peers index is ready")
	// connections
	handshaker := peers.NewHandshaker(n.cfg.Logger, n.streamCtrl, n.idx)
	n.host.SetStreamHandler(peers.HandshakeProtocol, handshaker.Handler())
	n.logger.Debug("handshaker is ready")

	connHandler := peers.HandleConnections(n.ctx, n.cfg.Logger, handshaker)
	n.host.Network().Notify(connHandler)
	n.logger.Debug("connection handler is ready")

	if err := n.setupDiscovery(); err != nil {
		return errors.Wrap(err, "failed to create discovery service")
	}
	n.logger.Debug("discovery is ready")

	if err := n.setupPubsub(); err != nil {
		return errors.Wrap(err, "failed to create discovery service")
	}
	n.logger.Debug("pubsub is ready")

	if err := n.setupSyncHandlers(); err != nil {
		return errors.Wrap(err, "failed to create discovery service")
	}
	n.logger.Debug("sync handlers are ready")

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
		Logger: n.cfg.Logger,
		Host:   n.host,
		Connect: func(info *peer.AddrInfo) error {
			ctx, cancel := context.WithTimeout(n.ctx, n.cfg.RequestTimeout)
			defer cancel()
			return n.host.Connect(ctx, *info)
		},
		DiscV5Opts: discV5Opts,
		ConnIndex:  n.idx,
	}
	disc, err := discovery.NewService(n.ctx, discOpts)
	if err != nil {
		return err
	}
	n.disc = disc

	return nil
}

func (n *p2pNetwork) setupPubsub() error {
	return nil
}

func (n *p2pNetwork) setupSyncHandlers() error {
	return nil
}
