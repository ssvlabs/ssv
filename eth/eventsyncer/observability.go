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

	// verifyCursorGauge reports the top block of the most recently verified chunk. With multiple
	// pending ranges it is not a global watermark: it jumps between ranges as their chunks are
	// verified, so read it as recent progress rather than a monotonic high-water mark.
	verifyCursorGauge = metrics.New(
		meter.Int64Gauge(
			observability.InstrumentName(observabilityNamespace, "cursor"),
			metric.WithDescription("top block of the most recently verified chunk (jumps between ranges when several are pending)")))

	// verifyMissCounter counts receipts-confirmed misses (a block whose receipts hold logs the sync
	// never recorded). Any increment schedules a full resync — it should stay at zero.
	verifyMissCounter = metrics.New(
		meter.Int64Counter(
			observability.InstrumentName(observabilityNamespace, "misses"),
			metric.WithDescription("registry-event completeness misses confirmed by the background verifier")))

	// verifyUnsettledBlocksGauge reports how many blocks no source has authoritatively verified
	// yet. Nonzero means the node may be on incomplete state for those blocks — typically because
	// no execution client can serve their receipts; investigate the execution client.
	verifyUnsettledBlocksGauge = metrics.New(
		meter.Int64Gauge(
			observability.InstrumentName(observabilityNamespace, "unsettled_blocks"),
			metric.WithDescription("blocks awaiting authoritative verification")))

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

func recordVerifyUnsettledBlocks(ctx context.Context, n int) {
	verifyUnsettledBlocksGauge.Record(ctx, int64(n))
}

func recordVerifySuppressed(ctx context.Context) {
	verifySuppressedCounter.Add(ctx, 1)
}
