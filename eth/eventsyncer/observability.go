package eventsyncer

import (
	"context"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/metric"

	"github.com/ssvlabs/ssv/observability"
	"github.com/ssvlabs/ssv/observability/metrics"
)

const (
	observabilityName      = "github.com/ssvlabs/ssv/eth/eventsyncer"
	observabilityNamespace = "ssv.event_syncer.verify"
)

var (
	meter = otel.Meter(observabilityName)

	// verifyPendingRangesGauge reports how many optimistically-synced ranges still await
	// background completeness verification. A nonzero value means the node's registry state
	// may be temporarily incomplete for those ranges.
	verifyPendingRangesGauge = metrics.New(
		meter.Int64Gauge(
			observability.InstrumentName(observabilityNamespace, "pending_ranges"),
			metric.WithDescription("number of optimistically-synced ranges awaiting background verification")))

	// verifyCursorGauge reports the highest block the verifier has confirmed complete so far.
	verifyCursorGauge = metrics.New(
		meter.Int64Gauge(
			observability.InstrumentName(observabilityNamespace, "cursor"),
			metric.WithDescription("highest block confirmed complete by the background verifier")))

	// verifyMissCounter counts receipts-confirmed misses (a block whose recorded digest disagreed
	// with the receipts truth). Any increment schedules a full resync — it should stay at zero.
	verifyMissCounter = metrics.New(
		meter.Int64Counter(
			observability.InstrumentName(observabilityNamespace, "misses"),
			metric.WithDescription("registry-event completeness misses confirmed by the background verifier")))

	// verifyParkedCounter counts ranges left pending because a disagreeing block couldn't be
	// authoritatively resolved (eth_getBlockReceipts unavailable). Nonzero means verification is
	// inconclusive — the node may be on incomplete state; investigate the execution client.
	verifyParkedCounter = metrics.New(
		meter.Int64Counter(
			observability.InstrumentName(observabilityNamespace, "parked"),
			metric.WithDescription("ranges left pending because a block couldn't be authoritatively verified")))

	// verifySuppressedCounter counts confirmed misses whose resync was suppressed by the rate
	// limit. Nonzero means a repair is being deferred — investigate the execution client.
	verifySuppressedCounter = metrics.New(
		meter.Int64Counter(
			observability.InstrumentName(observabilityNamespace, "suppressed"),
			metric.WithDescription("confirmed misses whose automatic resync was rate-limited")))
)

func recordVerifyPendingRanges(ctx context.Context, n int) {
	verifyPendingRangesGauge.Record(ctx, int64(n))
}

func recordVerifyCursor(ctx context.Context, block uint64) {
	// #nosec G115 -- block numbers fit in int64
	verifyCursorGauge.Record(ctx, int64(block))
}

func recordVerifyMiss(ctx context.Context) {
	verifyMissCounter.Add(ctx, 1)
}

func recordVerifyParked(ctx context.Context) {
	verifyParkedCounter.Add(ctx, 1)
}

func recordVerifySuppressed(ctx context.Context) {
	verifySuppressedCounter.Add(ctx, 1)
}
