package builderendpoint

import (
	"context"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"

	"github.com/ssvlabs/ssv/observability"
	"github.com/ssvlabs/ssv/observability/metrics"
)

const (
	observabilityName      = "github.com/ssvlabs/ssv/mev/builderendpoint"
	observabilityNamespace = "ssv.mev.builder_endpoint"
)

type getHeaderCacheResult string

const (
	getHeaderCacheHit  getHeaderCacheResult = "hit"
	getHeaderCacheMiss getHeaderCacheResult = "miss"
)

type getHeaderResult string

const (
	getHeaderResultBid   getHeaderResult = "bid"
	getHeaderResultNoBid getHeaderResult = "no_bid"
	getHeaderResultError getHeaderResult = "error"
)

var (
	meter = otel.Meter(observabilityName)

	getHeaderRequestsCounter = metrics.New(
		meter.Int64Counter(
			observability.InstrumentName(observabilityNamespace, "get_header.requests"),
			metric.WithUnit("{request}"),
			metric.WithDescription("total number of get_header requests handled by the builder endpoint")))

	getHeaderDurationHistogram = metrics.New(
		meter.Float64Histogram(
			observability.InstrumentName(observabilityNamespace, "get_header.duration"),
			metric.WithUnit("s"),
			metric.WithDescription("duration of get_header handling in seconds"),
			metric.WithExplicitBucketBoundaries(metrics.SecondsHistogramBuckets...)))

	getHeaderSlotOffsetHistogram = metrics.New(
		meter.Float64Histogram(
			observability.InstrumentName(observabilityNamespace, "get_header.slot_offset"),
			metric.WithUnit("s"),
			metric.WithDescription("time since slot start when get_header was handled (seconds into slot)"),
			metric.WithExplicitBucketBoundaries(metrics.SecondsHistogramBuckets...)))

	cacheEntriesGauge = metrics.New(
		meter.Int64Gauge(
			observability.InstrumentName(observabilityNamespace, "cache.entries"),
			metric.WithUnit("{entry}"),
			metric.WithDescription("number of bid entries held in the in-memory cache")))

	cacheProvenanceEntriesGauge = metrics.New(
		meter.Int64Gauge(
			observability.InstrumentName(observabilityNamespace, "cache.provenance_entries"),
			metric.WithUnit("{entry}"),
			metric.WithDescription("number of provenance entries held in the in-memory cache")))

	prefetchInFlightGauge = metrics.New(
		meter.Int64Gauge(
			observability.InstrumentName(observabilityNamespace, "prefetch.in_flight"),
			metric.WithUnit("{prefetch}"),
			metric.WithDescription("number of in-flight bid prefetch operations")))

	prefetchLeadTimeHistogram = metrics.New(
		meter.Float64Histogram(
			observability.InstrumentName(observabilityNamespace, "prefetch.lead_time"),
			metric.WithUnit("s"),
			metric.WithDescription("time before slot start when a prefetch request was issued (seconds)"),
			metric.WithExplicitBucketBoundaries(metrics.SecondsHistogramBuckets...)))

	prefetchLateCounter = metrics.New(
		meter.Int64Counter(
			observability.InstrumentName(observabilityNamespace, "prefetch.late"),
			metric.WithUnit("{prefetch}"),
			metric.WithDescription("number of prefetch requests issued after slot start")))
)

func getHeaderAttributes(cacheRes getHeaderCacheResult, res getHeaderResult) []attribute.KeyValue {
	return []attribute.KeyValue{
		attribute.String("ssv.mev.builder_endpoint.get_header.cache", string(cacheRes)),
		attribute.String("ssv.mev.builder_endpoint.get_header.result", string(res)),
	}
}

func recordGetHeader(ctx context.Context, mode string, cacheRes getHeaderCacheResult, res getHeaderResult, duration time.Duration) {
	attr := getHeaderAttributes(cacheRes, res)
	if mode != "" {
		attr = append(attr, attribute.String("ssv.mev.builder_endpoint.mode", mode))
	}
	getHeaderRequestsCounter.Add(ctx, 1, metric.WithAttributes(attr...))
	getHeaderDurationHistogram.Record(ctx, duration.Seconds(), metric.WithAttributes(attr...))
}

func recordGetHeaderSlotOffset(ctx context.Context, mode string, cacheRes getHeaderCacheResult, res getHeaderResult, offset time.Duration) {
	attr := getHeaderAttributes(cacheRes, res)
	if mode != "" {
		attr = append(attr, attribute.String("ssv.mev.builder_endpoint.mode", mode))
	}
	// Negative offsets can happen for future slots; clamp to 0 for clearer dashboards.
	if offset < 0 {
		offset = 0
	}
	getHeaderSlotOffsetHistogram.Record(ctx, offset.Seconds(), metric.WithAttributes(attr...))
}

func recordCacheGauges(ctx context.Context, bidEntries int, provenanceEntries int, inFlightPrefetch int) {
	cacheEntriesGauge.Record(ctx, int64(bidEntries))
	cacheProvenanceEntriesGauge.Record(ctx, int64(provenanceEntries))
	prefetchInFlightGauge.Record(ctx, int64(inFlightPrefetch))
}

func recordPrefetchLeadTime(ctx context.Context, lead time.Duration) {
	if lead < 0 {
		prefetchLateCounter.Add(ctx, 1)
		lead = 0
	}
	prefetchLeadTimeHistogram.Record(ctx, lead.Seconds())
}
