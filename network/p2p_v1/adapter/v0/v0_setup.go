package v0

import (
	"github.com/bloxapp/ssv/network/p2p_v1/discovery"
	"github.com/bloxapp/ssv/network/p2p_v1/topics"
	"github.com/libp2p/go-libp2p-core/peer"
	"github.com/pkg/errors"
	"go.uber.org/zap"
)

func (n *NetV0Adapter) setupDiscovery() error {
	// TODO: create new discovery.Service
	return n.disc.Bootstrap(func(e discovery.PeerEvent) {
		// TODO: check if relevant
		if err := n.host.Connect(n.ctx, e.AddrInfo); err != nil {
			n.v1Cfg.Logger.Warn("could not connect peer",
				zap.String("peer", e.AddrInfo.String()), zap.Error(err))
			return
		}
		n.v1Cfg.Logger.Debug("connected peer",
			zap.String("peer", e.AddrInfo.String()))
	})
}

func (n *NetV0Adapter) setupPubsub() error {
	var staticPeers []peer.AddrInfo
	if len(n.v0Cfg.ExporterPeerID) > 0 {
		expID, err := peer.Decode(n.v0Cfg.ExporterPeerID)
		if err != nil {
			return errors.Wrap(err, "could not decode exporter id")
		}
		staticPeers = append(staticPeers, peer.AddrInfo{ID: expID})
	}
	psBundle, err := topics.NewPubsub(n.ctx, &topics.PububConfig{
		Logger:      n.v1Cfg.Logger,
		Host:        n.host,
		TraceLog:    n.v1Cfg.PubSubTrace,
		StaticPeers: staticPeers,
		UseMsgID:    true,
		// TODO: check if needed
		//MsgValidatorFactory: func(s string) topics.MsgValidatorFunc {
		//	logger := n.cfg.Logger.With(zap.String("who", "MsgValidator"))
		//	return topics.NewSSVMsgValidator(logger, n.cfg.Fork, n.host.ID())
		//},
		MsgHandler: n.HandleMsg,
	})
	if err != nil {
		return errors.Wrap(err, "could not setup pubsub")
	}
	n.topicsCtrl = psBundle.TopicsCtrl

	return nil
}
