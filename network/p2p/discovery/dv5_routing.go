package discovery

import (
	"context"
	"github.com/libp2p/go-libp2p-core/discovery"
	"github.com/libp2p/go-libp2p-core/peer"
	"github.com/pkg/errors"
	"go.uber.org/zap"
	"time"
)

// implementing discovery.Discovery

// Advertise advertises a service
// implementation of discovery.Advertiser
func (dvs *DiscV5Service) Advertise(ctx context.Context, ns string, opt ...discovery.Option) (time.Duration, error) {
	opts := discovery.Options{}
	if err := opts.Apply(opt...); err != nil {
		return 0, errors.Wrap(err, "could not apply options")
	}
	if opts.Ttl == 0 {
		opts.Ttl = time.Hour
	}
	if !isSubnet(ns) {
		dvs.logger.Debug("not a subnet", zap.String("ns", ns))
		return opts.Ttl, nil
	}
	subnet := nsToSubnet(ns)

	if err := dvs.RegisterSubnets(int(subnet)); err != nil {
		return 0, err
	}

	return opts.Ttl, nil
}

// FindPeers discovers peers providing a service
// implementation of discovery.Discoverer
func (dvs *DiscV5Service) FindPeers(ctx context.Context, ns string, opt ...discovery.Option) (<-chan peer.AddrInfo, error) {
	if !isSubnet(ns) {
		dvs.logger.Debug("not a subnet", zap.String("ns", ns))
		return nil, nil
	}
	cn := make(chan peer.AddrInfo, 32)
	subnet := nsToSubnet(ns)
	dvs.discover(ctx, func(e PeerEvent) {
		cn <- e.AddrInfo
	}, time.Millisecond, dvs.badNodeFilter, dvs.findBySubnetFilter(subnet))

	return cn, nil
}
