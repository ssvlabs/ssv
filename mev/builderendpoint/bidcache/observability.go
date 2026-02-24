package bidcache

import (
	"context"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"

	"github.com/ssvlabs/ssv/observability"
	"github.com/ssvlabs/ssv/observability/metrics"
)

const (
	observabilityName      = "github.com/ssvlabs/ssv/mev/builderendpoint/bidcache"
	observabilityNamespace = "ssv.mev.builder_endpoint"
)

type prefetchSkipReason string

const (
	prefetchSkipReasonWarm     prefetchSkipReason = "warm"
	prefetchSkipReasonInFlight prefetchSkipReason = "in_flight"
	prefetchSkipReasonLimit    prefetchSkipReason = "limit"
)

type prefetchResult string

const (
	prefetchResultCached prefetchResult = "cached"
	prefetchResultNoBid  prefetchResult = "no_bid"
	prefetchResultError  prefetchResult = "error"
)

var (
	meter = otel.Meter(observabilityName)

	prefetchRequestsCounter = metrics.New(
		meter.Int64Counter(
			observability.InstrumentName(observabilityNamespace, "prefetch.requests"),
			metric.WithUnit("{request}"),
			metric.WithDescription("number of bid prefetch requests issued by the node")))

	prefetchSkipsCounter = metrics.New(
		meter.Int64Counter(
			observability.InstrumentName(observabilityNamespace, "prefetch.skips"),
			metric.WithUnit("{skip}"),
			metric.WithDescription("number of bid prefetch requests skipped")))

	prefetchResultsCounter = metrics.New(
		meter.Int64Counter(
			observability.InstrumentName(observabilityNamespace, "prefetch.results"),
			metric.WithUnit("{result}"),
			metric.WithDescription("outcomes of bid prefetch operations")))
)

func recordPrefetchRequest(ctx context.Context) {
	prefetchRequestsCounter.Add(ctx, 1)
}

func recordPrefetchSkip(ctx context.Context, reason prefetchSkipReason) {
	prefetchSkipsCounter.Add(ctx, 1, metric.WithAttributes(attribute.String("ssv.mev.builder_endpoint.prefetch.skip_reason", string(reason))))
}

func recordPrefetchResult(ctx context.Context, res prefetchResult) {
	prefetchResultsCounter.Add(ctx, 1, metric.WithAttributes(attribute.String("ssv.mev.builder_endpoint.prefetch.result", string(res))))
}
