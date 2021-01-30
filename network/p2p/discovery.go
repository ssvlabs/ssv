package p2p

import (
	"context"

	"github.com/libp2p/go-libp2p-core/host"
	"github.com/libp2p/go-libp2p-core/peer"
	"github.com/libp2p/go-libp2p/p2p/discovery"
	"github.com/pkg/errors"
	"go.uber.org/zap"
)

// discoveryNotifee gets notified when we find a new peer via mDNS discovery
type discoveryNotifee struct {
	host   host.Host
	logger *zap.Logger
}

// HandlePeerFound connects to peers discovered via mDNS. Once they're connected,
// the PubSub system will automatically start interacting with them if they also
// support PubSub.
func (n *discoveryNotifee) HandlePeerFound(pi peer.AddrInfo) {
	n.logger.Info("discovered new peer", zap.String("peer_id", pi.ID.Pretty()))
	err := n.host.Connect(context.Background(), pi)
	if err != nil {
		n.logger.Error("error connecting to peer", zap.String("peer_id", pi.ID.Pretty()), zap.Error(err))
	}
}

// setupDiscovery creates an mDNS discovery service and attaches it to the libp2p Host.
// This lets us automatically discover peers on the same LAN and connect to them.
func setupDiscovery(ctx context.Context, logger *zap.Logger, host host.Host) error {
	disc, err := discovery.NewMdnsService(ctx, host, DiscoveryInterval, DiscoveryServiceTag)
	if err != nil {
		return errors.Wrap(err, "failed to create new mDNS service")
	}

	disc.RegisterNotifee(&discoveryNotifee{
		host:   host,
		logger: logger,
	})

	return nil
}
