package v0

import (
	"github.com/bloxapp/ssv/network/commons"
	"github.com/bloxapp/ssv/network/p2p_v1/discovery"
	"github.com/bloxapp/ssv/network/p2p_v1/peers"
	"github.com/bloxapp/ssv/network/p2p_v1/topics"
	"github.com/libp2p/go-libp2p"
	"github.com/libp2p/go-libp2p-core/peer"
	"github.com/libp2p/go-libp2p/p2p/protocol/identify"
	"github.com/pkg/errors"
	"time"
)

// SetupHost configures a libp2p host
func (n *netV0Adapter) setupHost() error {
	opts, err := n.v1Cfg.Libp2pOptions()
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

func (n *netV0Adapter) setupDiscovery() error {
	ipAddr, err := commons.IPAddr()
	if err != nil {
		return errors.Wrap(err, "could not get ip addr")
	}
	var discV5Opts *discovery.DiscV5Options
	if len(n.v1Cfg.Bootnodes) > 0 { // otherwise, we are in local scenario
		discV5Opts = &discovery.DiscV5Options{
			IP:         ipAddr.String(),
			BindIP:     "", // net.IPv4zero.String()
			Port:       n.v1Cfg.UDPPort,
			TCPPort:    n.v1Cfg.TCPPort,
			NetworkKey: n.v1Cfg.NetworkPrivateKey,
			Bootnodes:  n.v1Cfg.TransformBootnodes(),
			Logger:     n.logger,
		}
	}
	discOpts := discovery.Options{
		Logger:     n.logger,
		Host:       n.host,
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

func (n *netV0Adapter) setupPubsub() error {
	var staticPeers []peer.AddrInfo
	if len(n.v0Cfg.ExporterPeerID) > 0 {
		expID, err := peer.Decode(n.v0Cfg.ExporterPeerID)
		if err != nil {
			return errors.Wrap(err, "could not decode exporter id")
		}
		staticPeers = append(staticPeers, peer.AddrInfo{ID: expID})
	}
	_, tc, err := topics.NewPubsub(n.ctx, &topics.PububConfig{
		Logger:      n.logger,
		Host:        n.host,
		TraceLog:    n.v1Cfg.PubSubTrace,
		StaticPeers: staticPeers,
		MsgHandler:  n.HandleMsg,
		Fork:        n.fork,
	})
	if err != nil {
		return errors.Wrap(err, "could not setup pubsub")
	}
	n.topicsCtrl = tc

	return nil
}

func (n *netV0Adapter) setupPeerServices() error {
	self := peers.NewIdentity(n.host.ID().String(), "", "", make(map[string]string))
	n.idx = peers.NewPeersIndex(n.logger, n.host.Network(), self, func() int {
		return n.v1Cfg.MaxPeers
	}, 10*time.Minute)

	ids, err := identify.NewIDService(n.host, identify.UserAgent(n.v1Cfg.UserAgent))
	if err != nil {
		return errors.Wrap(err, "failed to create ID service")
	}

	handshaker := peers.NewHandshaker(n.ctx, n.logger, n.streamCtrl, n.idx, ids)
	n.host.SetStreamHandler(peers.HandshakeProtocol, handshaker.Handler())

	connHandler := peers.HandleConnections(n.ctx, n.logger, handshaker)
	n.host.Network().Notify(connHandler)
	return nil
}
