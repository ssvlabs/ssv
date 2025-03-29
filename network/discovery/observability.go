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

type skipReason string

const (
	skipReasonReachedLimit       skipReason = "reachedLimit"
	skipReasonNoSharedSubnets    skipReason = "noSharedSubnets"
	skipReasonZeroSubnets        skipReason = "zeroSubnets"
	skipReasonDomainTypeMismatch skipReason = "domainTypeMismatch"
)

var (
	meter = otel.Meter(observabilityName)

	peerDiscoveryIterationsCounter = observability.NewMetric(
		meter.Int64Counter(
			metricName("iterations"),
			metric.WithUnit("{iteration}"),
			metric.WithDescription("total number of iterations through discovered nodes")))

	peerDiscoveriesCounter = observability.NewMetric(
		meter.Int64Counter(
			metricName("peers"),
			metric.WithUnit("{peer}"),
			metric.WithDescription("total number of peers discovered")))

	peerRejectionsCounter = observability.NewMetric(
		meter.Int64Counter(
			metricName("peers.skipped"),
			metric.WithUnit("{peer}"),
			metric.WithDescription("total number of peers skipped during discovery")))

	peerAcceptedCounter = observability.NewMetric(
		meter.Int64Counter(
			metricName("peers.accepted"),
			metric.WithUnit("{peer}"),
			metric.WithDescription("total number of peers accepted during discovery")))
)

func metricName(name string) string {
	return fmt.Sprintf("%s.%s", observabilityNamespace, name)
}

func recordPeerSkipped(ctx context.Context, reason skipReason) {
	peerRejectionsCounter.Add(ctx, 1, metric.WithAttributes(peerSkipReasonAttribute(reason)))
}

func peerSkipReasonAttribute(reason skipReason) attribute.KeyValue {
	return attribute.String("ssv.p2p.discovery.skip_reason", string(reason))
}
