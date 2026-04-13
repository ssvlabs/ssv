package discovery

import (
	"context"
	"time"

	"github.com/libp2p/go-libp2p/core/discovery"
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/pkg/errors"
	"go.uber.org/zap"

	"github.com/ssvlabs/ssv/observability/log"
	"github.com/ssvlabs/ssv/observability/log/fields"
)

// implementing discovery.Discovery

// Advertise advertises a service
// implementation of discovery.Advertiser
func (dvs *DiscV5Service) Advertise(ctx context.Context, ns string, opt ...discovery.Option) (time.Duration, error) {
	logger := log.FromContext(ctx).Named(log.NameDiscoveryService)
	opts := discovery.Options{}
	if err := opts.Apply(opt...); err != nil {
		return 0, errors.Wrap(err, "could not apply options")
	}
	if opts.Ttl == 0 {
		opts.Ttl = time.Hour
	}
	subnet, err := dvs.nsToSubnet(ns)
	if err != nil {
		logger.Debug("not a subnet", fields.Topic(ns), zap.Error(err))
		return opts.Ttl, nil
	}

	updated, err := dvs.RegisterSubnets(subnet)
	if err != nil {
		return 0, err
	}
	if updated {
		go dvs.PublishENR()
	}

	return opts.Ttl, nil
}

// FindPeers discovers peers providing a service implementation of discovery.Discoverer.
// Called by libp2p pubsub (via pubsub.WithDiscovery) to find additional peers for a
// specific subnet/topic.
//
// Unlike Bootstrap — which builds a broad candidate pool scored by subnet coverage —
// FindPeers is a targeted, on-demand search: pubsub needs peers for one specific
// subnet it is trying to gossip on. This drives the filter differences:
//
//   - subnetFilter(subnet) instead of sharedSubnetsFilter: pubsub asks "give me peers
//     for subnet X", not "peers sharing any subnet with me".
//   - No alreadyDiscoveredFilter: pubsub may call FindPeers repeatedly for the same
//     subnet and expects fresh candidate lists; there is no candidate pool to dedup against.
//   - ssvNodeFilter, badNodeFilter, alreadyConnectedFilter, recentlyTrimmedFilter:
//     same as Bootstrap — basic validity and connection-state checks still apply.
func (dvs *DiscV5Service) FindPeers(ctx context.Context, ns string, opt ...discovery.Option) (<-chan peer.AddrInfo, error) {
	logger := log.FromContext(ctx).Named(log.NameDiscoveryService)
	subnet, err := dvs.nsToSubnet(ns)
	if err != nil {
		logger.Debug("not a subnet", fields.Topic(ns), zap.Error(err))
		return nil, nil
	}
	cn := make(chan peer.AddrInfo, 32)

	dvs.discover(ctx, func(e PeerEvent) {
		cn <- e.AddrInfo
	}, time.Millisecond, dvs.ssvNodeFilter(), dvs.badNodeFilter(), dvs.subnetFilter(subnet), dvs.alreadyConnectedFilter(), dvs.recentlyTrimmedFilter())

	return cn, nil
}
