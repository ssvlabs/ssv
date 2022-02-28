package p2pv1

import (
	"context"
	"github.com/bloxapp/ssv/network/p2p_v1/discovery"
	"github.com/bloxapp/ssv/network/p2p_v1/peers"
	"github.com/bloxapp/ssv/network/p2p_v1/streams"
	"github.com/libp2p/go-libp2p"
	"github.com/libp2p/go-libp2p-core/peer"
	"github.com/libp2p/go-libp2p/p2p/protocol/identify"
	"github.com/pkg/errors"
	"time"
)

// Setup is used to setup the network
func (n *p2pNetwork) Setup() error {
	err := n.SetupHost()
	if err != nil {
		return err
	}

	err = n.SetupServices()
	if err != nil {
		return err
	}

	err = n.watchPeers()
	if err != nil {
		return err
	}
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

// SetupServices configures the needed services
func (n *p2pNetwork) SetupServices() error {
	n.streamCtrl = streams.NewStreamController(n.ctx, n.logger, n.host, n.cfg.Fork, n.cfg.RequestTimeout)
	ids, err := identify.NewIDService(n.host, identify.UserAgent(userAgent()))
	if err != nil {
		return errors.Wrap(err, "failed to create ID service")
	}
	n.ids = ids

	self := peers.NewIdentity(n.host.ID().String(), "", "", make(map[string]string))
	n.idx = peers.NewPeersIndex(n.cfg.Logger, n.host.Network(), self, func() int {
		return n.cfg.MaxPeers
	}, 10*time.Minute)
	// connections
	handshaker := peers.NewHandshaker(n.cfg.Logger, n.streamCtrl, n.idx)
	n.host.SetStreamHandler(peers.HandshakeProtocol, handshaker.Handler())
	connHandler := peers.HandleConnections(n.ctx, n.cfg.Logger, handshaker)
	n.host.Network().Notify(connHandler)

	if err := n.setupDiscovery(); err != nil {
		return errors.Wrap(err, "failed to create discovery service")
	}
	if err := n.setupPubsub(); err != nil {
		return errors.Wrap(err, "failed to create discovery service")
	}
	if err := n.setupSyncHandlers(); err != nil {
		return errors.Wrap(err, "failed to create discovery service")
	}

	return nil
}

func (n *p2pNetwork) setupDiscovery() error {
	discOpts := discovery.Options{
		Logger: n.cfg.Logger,
		//Type:       "",
		//Host:       nil,
		Connect: func(info *peer.AddrInfo) error {
			ctx, cancel := context.WithTimeout(n.ctx, n.cfg.RequestTimeout)
			defer cancel()
			return n.host.Connect(ctx, *info)
		},
		DiscV5Opts: &discovery.DiscV5Options{},
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

func (n *p2pNetwork) watchPeers() error {
	return nil
}
