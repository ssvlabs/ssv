package discovery

import (
	"context"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"

	"github.com/ssvlabs/ssv/v2/observability"
	"github.com/ssvlabs/ssv/v2/observability/metrics"
)

const (
	// Pre-/v2 path on purpose: exported as the otel_scope_name metric label.
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
		meter.Int64ObservableGauge(
			observability.InstrumentName(observabilityNamespace, "socket.read_staleness"),
			metric.WithUnit("s"),
			metric.WithDescription("seconds since the discv5 socket was last read; -1 until the first read")))
)

func recordUnhandledPacketDropped(ctx context.Context) {
	unhandledPacketsDroppedCounter.Add(ctx, 1)
}

// observeDiscoveryReadStaleness registers a callback reporting conn's read
// staleness at scrape time, so the series keeps reporting even when the health
// check short-circuits before reaching it. The caller unregisters it on close.
func observeDiscoveryReadStaleness(conn *TimedConn) (metric.Registration, error) {
	return meter.RegisterCallback(func(_ context.Context, observer metric.Observer) error {
		observer.ObserveInt64(discoveryReadStalenessGauge, readStalenessSeconds(conn))
		return nil
	}, discoveryReadStalenessGauge)
}

// readStalenessSeconds flattens ReadStaleness for the gauge, reporting -1 before
// the first read so never-read stays distinct from just-read (0).
func readStalenessSeconds(conn *TimedConn) int64 {
	age, everRead := conn.ReadStaleness()
	if !everRead {
		return -1
	}
	return int64(age.Seconds())
}

func recordPeerSkipped(ctx context.Context, reason skipReason) {
	peerRejectionsCounter.Add(ctx, 1, metric.WithAttributes(peerSkipReasonAttribute(reason)))
}

func peerSkipReasonAttribute(reason skipReason) attribute.KeyValue {
	return attribute.String("ssv.p2p.discovery.skip_reason", string(reason))
}
