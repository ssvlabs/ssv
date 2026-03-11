package unblinder

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
	observabilityName      = "github.com/ssvlabs/ssv/mev/builderendpoint/unblinder"
	observabilityNamespace = "ssv.mev.builder_endpoint"
)

type unblindMode string

const (
	unblindModeFanout     unblindMode = "fanout"
	unblindModeProvenance unblindMode = "provenance"
)

type unblindResult string

const (
	unblindResultSuccess   unblindResult = "success"
	unblindResultNoPayload unblindResult = "no_payload"
	unblindResultError     unblindResult = "error"
)

var (
	meter = otel.Meter(observabilityName)

	unblindRequestsCounter = metrics.New(
		meter.Int64Counter(
			observability.InstrumentName(observabilityNamespace, "unblind.requests"),
			metric.WithUnit("{request}"),
			metric.WithDescription("number of unblind requests handled by the builder endpoint")))

	unblindDurationHistogram = metrics.New(
		meter.Float64Histogram(
			observability.InstrumentName(observabilityNamespace, "unblind.duration"),
			metric.WithUnit("s"),
			metric.WithDescription("duration of unblind operations in seconds"),
			metric.WithExplicitBucketBoundaries(metrics.SecondsHistogramBuckets...)))

	unblindProvenanceLookupsCounter = metrics.New(
		meter.Int64Counter(
			observability.InstrumentName(observabilityNamespace, "unblind.provenance_lookups"),
			metric.WithUnit("{lookup}"),
			metric.WithDescription("number of provenance lookups performed for unblinding")))
)

func recordProvenanceLookup(ctx context.Context, hit bool) {
	if ctx == nil {
		ctx = context.Background()
	}
	unblindProvenanceLookupsCounter.Add(ctx, 1, metric.WithAttributes(attribute.Bool("ssv.mev.builder_endpoint.unblind.provenance_hit", hit)))
}

func recordUnblind(ctx context.Context, mode unblindMode, res unblindResult, duration time.Duration) {
	if ctx == nil {
		ctx = context.Background()
	}
	attr := []attribute.KeyValue{
		attribute.String("ssv.mev.builder_endpoint.unblind.mode", string(mode)),
		attribute.String("ssv.mev.builder_endpoint.unblind.result", string(res)),
	}
	unblindRequestsCounter.Add(ctx, 1, metric.WithAttributes(attr...))
	unblindDurationHistogram.Record(ctx, duration.Seconds(), metric.WithAttributes(attr...))
}
