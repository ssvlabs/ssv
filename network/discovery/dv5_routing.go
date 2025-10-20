package discovery

import (
	"context"
	"time"

	"github.com/libp2p/go-libp2p/core/discovery"
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/pkg/errors"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
	"go.uber.org/zap"

	"github.com/ssvlabs/ssv/observability"
	"github.com/ssvlabs/ssv/observability/log"
	"github.com/ssvlabs/ssv/observability/log/fields"
)

// implementing discovery.Discovery

// Advertise advertises a service
// implementation of discovery.Advertiser
func (dvs *DiscV5Service) Advertise(ctx context.Context, ns string, opt ...discovery.Option) (time.Duration, error) {
	logger := log.FromContext(ctx).Named(log.NameDiscoveryService)
	ctx, span := tracer.Start(ctx,
		observability.InstrumentName(observabilityNamespace, "discovery.advertise"),
	)
	defer span.End()

	opts := discovery.Options{}
	if err := opts.Apply(opt...); err != nil {
		span.SetStatus(codes.Error, err.Error())
		return 0, errors.Wrap(err, "could not apply options")
	}
	if opts.Ttl == 0 {
		opts.Ttl = time.Hour
	}
	subnet, err := dvs.nsToSubnet(ns)
	if err != nil {
		logger.Debug("not a subnet", fields.Topic(ns), zap.Error(err))
		span.AddEvent("not_a_subnet", trace.WithAttributes(attribute.String("ssv.p2p.topic.name", ns)))
		return opts.Ttl, nil
	}

	updated, err := dvs.RegisterSubnets(subnet)
	if err != nil {
		span.SetStatus(codes.Error, err.Error())
		return 0, err
	}
	if updated {
		span.AddEvent("subnets_registered", trace.WithAttributes(attribute.Int64("ssv.p2p.subnet", int64(subnet))))
		go dvs.PublishENR()
	}

	span.SetStatus(codes.Ok, "")
	return opts.Ttl, nil
}

// FindPeers discovers peers providing a service implementation of discovery.Discoverer
func (dvs *DiscV5Service) FindPeers(ctx context.Context, ns string, opt ...discovery.Option) (<-chan peer.AddrInfo, error) {
	logger := log.FromContext(ctx).Named(log.NameDiscoveryService)
	ctx, span := tracer.Start(ctx,
		observability.InstrumentName(observabilityNamespace, "discovery.find_peers"),
		// Note: filter application is traced in discover(); here we annotate the namespace.
	)
	defer span.End()
	subnet, err := dvs.nsToSubnet(ns)
	if err != nil {
		logger.Debug("not a subnet", fields.Topic(ns), zap.Error(err))
		span.AddEvent("not_a_subnet", trace.WithAttributes(attribute.String("ssv.p2p.topic.name", ns)))
		return nil, nil
	}
	cn := make(chan peer.AddrInfo, 32)

	dvs.discover(ctx, func(e PeerEvent) {
		cn <- e.AddrInfo
	}, time.Millisecond, dvs.ssvNodeFilter(), dvs.badNodeFilter(), dvs.subnetFilter(subnet), dvs.alreadyConnectedFilter(), dvs.recentlyTrimmedFilter())

	span.SetStatus(codes.Ok, "")
	return cn, nil
}
