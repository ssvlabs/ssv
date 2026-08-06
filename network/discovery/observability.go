package discovery

import (
	"context"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"

	"github.com/ssvlabs/ssv/observability"
	"github.com/ssvlabs/ssv/observability/metrics"
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
	skipReasonInvalidDomainType  skipReason = "invalidDomainType"
	skipReasonInvalidSubnets     skipReason = "invalidSubnets"
	skipReasonInvalidPeerID      skipReason = "invalidPeerID"
	skipReasonAlreadyDiscovered  skipReason = "alreadyDiscovered"
	skipReasonAlreadyConnected   skipReason = "alreadyConnected"
	skipReasonBadPeer            skipReason = "badPeer"
	skipReasonNotSSV             skipReason = "notSSV"
	skipReasonRecentlyTrimmed    skipReason = "recentlyTrimmed"
)

var (
	meter = otel.Meter(observabilityName)

	peerDiscoveryIterationsCounter = metrics.New(
		meter.Int64Counter(
			observability.InstrumentName(observabilityNamespace, "iterations"),
			metric.WithUnit("{iteration}"),
			metric.WithDescription("total number of iterations through discovered nodes")))

	peerDiscoveriesCounter = metrics.New(
		meter.Int64Counter(
			observability.InstrumentName(observabilityNamespace, "peers"),
			metric.WithUnit("{peer}"),
			metric.WithDescription("total number of peers discovered")))

	peerRejectionsCounter = metrics.New(
		meter.Int64Counter(
			observability.InstrumentName(observabilityNamespace, "peers.skipped"),
			metric.WithUnit("{peer}"),
			metric.WithDescription("total number of peers skipped during discovery")))

	peerAcceptedCounter = metrics.New(
		meter.Int64Counter(
			observability.InstrumentName(observabilityNamespace, "peers.accepted"),
			metric.WithUnit("{peer}"),
			metric.WithDescription("total number of peers accepted during discovery")))

	unhandledPacketsDroppedCounter = metrics.New(
		meter.Int64Counter(
			observability.InstrumentName(observabilityNamespace, "unhandled_packets.dropped"),
			metric.WithUnit("{packet}"),
			metric.WithDescription("total number of packets forwarded to the pre-fork listener that were dropped because its buffer was full")))

	discoveryReadStalenessGauge = metrics.New(
		meter.Int64Gauge(
			observability.InstrumentName(observabilityNamespace, "socket.read_staleness"),
			metric.WithUnit("s"),
			metric.WithDescription("seconds since the discv5 socket was last read; 0 until the first read")))
)

func recordUnhandledPacketDropped(ctx context.Context) {
	unhandledPacketsDroppedCounter.Add(ctx, 1)
}

func recordDiscoveryReadStaleness(ctx context.Context, seconds int64) {
	discoveryReadStalenessGauge.Record(ctx, seconds)
}

func recordPeerSkipped(ctx context.Context, reason skipReason) {
	peerRejectionsCounter.Add(ctx, 1, metric.WithAttributes(peerSkipReasonAttribute(reason)))
}

func peerSkipReasonAttribute(reason skipReason) attribute.KeyValue {
	return attribute.String("ssv.p2p.discovery.skip_reason", string(reason))
}
