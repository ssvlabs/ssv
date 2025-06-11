package discovery

import (
	"context"
	"time"

	"github.com/libp2p/go-libp2p/core/discovery"
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/pkg/errors"
	"go.uber.org/zap"

	"github.com/ssvlabs/ssv/logging"
	"github.com/ssvlabs/ssv/logging/fields"
)

// implementing discovery.Discovery

// Advertise advertises a service
// implementation of discovery.Advertiser
func (dvs *DiscV5Service) Advertise(ctx context.Context, ns string, opt ...discovery.Option) (time.Duration, error) {
	logger := logging.FromContext(ctx).Named(logging.NameDiscoveryService)
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

	updated, err := dvs.RegisterSubnets(logger, subnet)
	if err != nil {
		return 0, err
	}
	if updated {
		go dvs.PublishENR(logger)
	}

	return opts.Ttl, nil
}

// FindPeers discovers peers providing a service implementation of discovery.Discoverer
func (dvs *DiscV5Service) FindPeers(ctx context.Context, ns string, opt ...discovery.Option) (<-chan peer.AddrInfo, error) {
	logger := logging.FromContext(ctx).Named(logging.NameDiscoveryService)
	subnet, err := dvs.nsToSubnet(ns)
	if err != nil {
		logger.Debug("not a subnet", fields.Topic(ns), zap.Error(err))
		return nil, nil
	}
	cn := make(chan peer.AddrInfo, 32)

	dvs.discover(ctx, func(e PeerEvent) {
		cn <- e.AddrInfo
	}, time.Millisecond, dvs.ssvNodeFilter(logger), dvs.badNodeFilter(logger), dvs.subnetFilter(subnet), dvs.alreadyConnectedFilter(), dvs.recentlyTrimmedFilter())

	return cn, nil
}
