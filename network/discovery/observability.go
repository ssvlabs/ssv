package discovery

import (
	"context"
	"fmt"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"

	"github.com/ssvlabs/ssv/observability"
)

const (
	observabilityName      = "github.com/ssvlabs/ssv/network/discovery"
	observabilityNamespace = "ssv.p2p.discovery"
)

type rejectionReason string

const (
	reachedLimitReason    rejectionReason = "reachedLimit"
	noSharedSubnetsReason rejectionReason = "noSharedSubnets"
	zeroSubnetsReason     rejectionReason = "zeroSubnets"
)

var (
	meter = otel.Meter(observabilityName)

	peerDiscoveriesCounter = observability.NewMetric(
		meter.Int64Counter(
			metricName("peers"),
			metric.WithUnit("{peer}"),
			metric.WithDescription("total number of peers discovered")))

	peerRejectionsCounter = observability.NewMetric(
		meter.Int64Counter(
			metricName("peers.rejected"),
			metric.WithUnit("{peer}"),
			metric.WithDescription("total number of peers rejected during discovery")))

	peerAcceptedCounter = observability.NewMetric(
		meter.Int64Counter(
			metricName("peers.accepted"),
			metric.WithUnit("{peer}"),
			metric.WithDescription("total number of peers accepted during discovery")))
)

func metricName(name string) string {
	return fmt.Sprintf("%s.%s", observabilityNamespace, name)
}

func recordPeerRejection(ctx context.Context, reason rejectionReason) {
	peerRejectionsCounter.Add(ctx, 1, metric.WithAttributes(peerRejectionReasonAttribute(reason)))
}

func peerRejectionReasonAttribute(reason rejectionReason) attribute.KeyValue {
	return attribute.String("ssv.p2p.discovery.rejection_reason", string(reason))
}
