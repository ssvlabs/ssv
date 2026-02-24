package bids

import (
	"context"
	"time"

	builderspec "github.com/attestantio/go-builder-client/spec"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
	semconv "go.opentelemetry.io/otel/semconv/v1.37.0"

	"github.com/ssvlabs/ssv/observability"
	"github.com/ssvlabs/ssv/observability/metrics"

	"github.com/ssvlabs/ssv/mev/builderendpoint/bidcache"
)

const (
	observabilityName      = "github.com/ssvlabs/ssv/mev/builderendpoint/bids"
	observabilityNamespace = "ssv.mev.builder_endpoint"
)

type bidFetchResult string

const (
	bidFetchResultBid   bidFetchResult = "bid"
	bidFetchResultNoBid bidFetchResult = "no_bid"
	bidFetchResultError bidFetchResult = "error"
)

var (
	meter = otel.Meter(observabilityName)

	bidFetchesCounter = metrics.New(
		meter.Int64Counter(
			observability.InstrumentName(observabilityNamespace, "bid_fetches"),
			metric.WithUnit("{fetch}"),
			metric.WithDescription("number of relay bid fetch operations")))

	bidFetchDurationHistogram = metrics.New(
		meter.Float64Histogram(
			observability.InstrumentName(observabilityNamespace, "bid_fetch.duration"),
			metric.WithUnit("s"),
			metric.WithDescription("duration of relay bid fetch operations in seconds"),
			metric.WithExplicitBucketBoundaries(metrics.SecondsHistogramBuckets...)))

	bidFetchWinnersCounter = metrics.New(
		meter.Int64Counter(
			observability.InstrumentName(observabilityNamespace, "bid_fetch.winners"),
			metric.WithUnit("{winner}"),
			metric.WithDescription("number of times a relay won bid selection")))
)

func bidFetchAttributes(source string, res bidFetchResult) []attribute.KeyValue {
	return []attribute.KeyValue{
		attribute.String("ssv.mev.builder_endpoint.bid_fetch.source", source),
		attribute.String("ssv.mev.builder_endpoint.bid_fetch.result", string(res)),
	}
}

// FetcherWithMetrics wraps a bidcache.Fetcher to record bid fetch timings and outcomes.
type FetcherWithMetrics struct {
	Source string
	Next   bidcache.Fetcher
}

func (f *FetcherWithMetrics) FetchBestBid(ctx context.Context, key bidcache.Key) (*builderspec.VersionedSignedBuilderBid, string, error) {
	if f == nil || f.Next == nil {
		return nil, "", nil
	}
	if ctx == nil {
		ctx = context.Background()
	}

	start := time.Now()
	bid, provenance, err := f.Next.FetchBestBid(ctx, key)
	duration := time.Since(start)

	result := bidFetchResultNoBid
	if err != nil {
		result = bidFetchResultError
	} else if bid != nil {
		result = bidFetchResultBid
	}

	attr := bidFetchAttributes(f.Source, result)
	bidFetchesCounter.Add(ctx, 1, metric.WithAttributes(attr...))
	bidFetchDurationHistogram.Record(ctx, duration.Seconds(), metric.WithAttributes(attr...))

	if err == nil && bid != nil && provenance != "" {
		// Relay addresses are configured and low-cardinality for a given operator.
		bidFetchWinnersCounter.Add(ctx, 1, metric.WithAttributes(append(attr, semconv.ServerAddress(provenance))...))
	}

	return bid, provenance, err
}
