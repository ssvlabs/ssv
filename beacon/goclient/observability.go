package goclient

import (
	"context"
	"errors"
	"fmt"
	"time"

	eth2api "github.com/attestantio/go-eth2-client/api"
	"github.com/attestantio/go-eth2-client/spec/phase0"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
	semconv "go.opentelemetry.io/otel/semconv/v1.37.0"
	"go.uber.org/zap"

	"github.com/ssvlabs/ssv/observability"
	"github.com/ssvlabs/ssv/observability/log/fields"
	"github.com/ssvlabs/ssv/observability/metrics"
)

type beaconNodeStatus string

const (
	observabilityName      = "github.com/ssvlabs/ssv/beacon/goclient"
	observabilityNamespace = "ssv.cl"

	statusUnknown beaconNodeStatus = "unknown"
	statusSyncing beaconNodeStatus = "syncing"
	statusSynced  beaconNodeStatus = "synced"
)

var (
	meter = otel.Meter(observabilityName)

	requestDurationHistogram = metrics.New(
		meter.Float64Histogram(
			observability.InstrumentName(observabilityNamespace, "request.duration"),
			metric.WithUnit("s"),
			metric.WithDescription("consensus client request duration in seconds"),
			metric.WithExplicitBucketBoundaries(metrics.SecondsHistogramBuckets...)))

	beaconNodeStatusGauge = metrics.New(
		meter.Int64Gauge(
			observability.InstrumentName(observabilityNamespace, "sync.status"),
			metric.WithDescription("beacon node status")))

	syncDistanceGauge = metrics.New(
		meter.Int64Gauge(
			observability.InstrumentName(observabilityNamespace, "sync.distance"),
			metric.WithUnit("{block}"),
			metric.WithDescription("consensus client syncing distance which is a delta between highest and current blocks")))

	attestationDataClientSelections = metrics.New(
		meter.Int64Counter(
			observability.InstrumentName(observabilityNamespace, "attestation_data.client_selections"),
			metric.WithUnit("{selection}"),
			metric.WithDescription("beacon client selections for attestation data")))

	// Head verification metrics (for stale attestation data detection)
	attestationDataHeadVerifyCounter = metrics.New(
		meter.Int64Counter(
			observability.InstrumentName(observabilityNamespace, "attestation_data.head_verify"),
			metric.WithUnit("{verification}"),
			metric.WithDescription("total attestation data fetches that attempted head verification")))

	attestationDataHeadMatchCounter = metrics.New(
		meter.Int64Counter(
			observability.InstrumentName(observabilityNamespace, "attestation_data.head_match"),
			metric.WithUnit("{match}"),
			metric.WithDescription("attestation data head matched cached HeadEvent")))

	attestationDataHeadCacheMissCounter = metrics.New(
		meter.Int64Counter(
			observability.InstrumentName(observabilityNamespace, "attestation_data.head_cache_miss"),
			metric.WithUnit("{miss}"),
			metric.WithDescription("head root was not cached (no verification performed)")))

	attestationDataHeadMismatchCounter = metrics.New(
		meter.Int64Counter(
			observability.InstrumentName(observabilityNamespace, "attestation_data.head_mismatch"),
			metric.WithUnit("{mismatch}"),
			metric.WithDescription("attestation data head did not match cached HeadEvent")))

	attestationDataRefetchSuccessCounter = metrics.New(
		meter.Int64Counter(
			observability.InstrumentName(observabilityNamespace, "attestation_data.refetch_success"),
			metric.WithUnit("{refetch}"),
			metric.WithDescription("re-fetch got correct head after mismatch")))

	attestationDataRefetchFailedCounter = metrics.New(
		meter.Int64Counter(
			observability.InstrumentName(observabilityNamespace, "attestation_data.refetch_failed"),
			metric.WithUnit("{refetch}"),
			metric.WithDescription("re-fetch failed or timed out")))

	attestationDataRefetchStillMismatchCounter = metrics.New(
		meter.Int64Counter(
			observability.InstrumentName(observabilityNamespace, "attestation_data.refetch_still_mismatch"),
			metric.WithUnit("{refetch}"),
			metric.WithDescription("re-fetch still had wrong head (possible reorg)")))

	attestationDataRefetchSkippedCounter = metrics.New(
		meter.Int64Counter(
			observability.InstrumentName(observabilityNamespace, "attestation_data.refetch_skipped"),
			metric.WithUnit("{skip}"),
			metric.WithDescription("retry skipped due to insufficient time before deadline")))

	// Proposal parent verification metrics (observability only, no re-fetch)
	proposalParentVerifyCounter = metrics.New(
		meter.Int64Counter(
			observability.InstrumentName(observabilityNamespace, "proposal.parent_verify"),
			metric.WithUnit("{verification}"),
			metric.WithDescription("total proposals that attempted parent root verification")))

	proposalParentMatchCounter = metrics.New(
		meter.Int64Counter(
			observability.InstrumentName(observabilityNamespace, "proposal.parent_match"),
			metric.WithUnit("{match}"),
			metric.WithDescription("proposal parent root matched cached HeadEvent")))

	proposalParentCacheMissCounter = metrics.New(
		meter.Int64Counter(
			observability.InstrumentName(observabilityNamespace, "proposal.parent_cache_miss"),
			metric.WithUnit("{miss}"),
			metric.WithDescription("parent slot head was not cached (no verification performed)")))

	proposalParentMismatchCounter = metrics.New(
		meter.Int64Counter(
			observability.InstrumentName(observabilityNamespace, "proposal.parent_mismatch"),
			metric.WithUnit("{mismatch}"),
			metric.WithDescription("proposal parent root did not match cached HeadEvent")))

	// Counts attestations we submitted successfully but could not remember the data root for, so
	// the aggregator flow silently degrades to the 404-prone re-derivation. Should stay at zero.
	attestedDataRootRememberFailedCounter = metrics.New(
		meter.Int64Counter(
			observability.InstrumentName(observabilityNamespace, "attestation_data.remember_root_failed"),
			metric.WithUnit("{failure}"),
			metric.WithDescription("submitted attestations whose data root could not be remembered for the aggregator flow")))
)

func recordRequest(
	ctx context.Context,
	logger *zap.Logger,
	routeName string,
	client interface{ Address() string },
	httpMethod string,
	maybeFallback bool, // whether this request might have undergone a fallback (true means maybe, not a 100% yes)
	duration time.Duration,
	err error,
) {
	// Log the request, but only if it has errored or if it took long enough that we want to pay attention
	// to it (there are too many requests being made to log them every time, some requests don't even result
	// into a network-based call due to caching implemented for some routes).
	if err != nil || duration > 1*time.Millisecond {
		success := "true"
		if err != nil {
			success = "false"
			if errors.Is(err, context.Canceled) {
				success = "unknown" // we don't know the outcome of this request
			}
		}
		logger.Debug("CL request done",
			zap.String("route_name", routeName),
			zap.String("client_addr", client.Address()),
			zap.String("http_method", httpMethod),
			zap.Bool("maybe_fallback", maybeFallback),
			fields.Took(duration),
			zap.String("success", success),
			zap.Error(err),
		)
	}

	// Build metric attributes, add error-code attribute in case there is an error (treat context.Canceled
	// as non-error since it means we've canceled the request ourselves).
	attr := []attribute.KeyValue{
		semconv.ServerAddress(client.Address()),
		semconv.HTTPRequestMethodKey.String(httpMethod),
		attribute.String("http.route_name", routeName),
	}
	if err != nil && !errors.Is(err, context.Canceled) {
		// Error code of 0 signifies the presence of some error, see if we can clarify if further by using
		// api error codes.
		errCode := 0
		var apiErr *eth2api.Error
		if errors.As(err, &apiErr) {
			errCode = apiErr.StatusCode
		}
		attr = append(attr, attribute.Int("http.response.error_status_code", errCode))
	}
	// Record the request as a metric.
	requestDurationHistogram.Record(
		ctx,
		duration.Seconds(),
		metric.WithAttributes(attr...))
}

func recordSyncDistance(ctx context.Context, distance phase0.Slot, serverAddr string) {
	metrics.RecordUint64Value(ctx, uint64(distance), syncDistanceGauge.Record, metric.WithAttributes(semconv.ServerAddress(serverAddr)))
}

func recordBeaconClientStatus(ctx context.Context, status beaconNodeStatus, serverAddr string) {
	resetBeaconClientStatusGauge(ctx, serverAddr)

	beaconNodeStatusGauge.Record(ctx, 1,
		metric.WithAttributes(semconv.ServerAddress(serverAddr)),
		metric.WithAttributes(beaconClientStatusAttribute(status)),
	)
}

func resetBeaconClientStatusGauge(ctx context.Context, serverAddr string) {
	for _, status := range []beaconNodeStatus{statusSynced, statusSyncing, statusUnknown} {
		beaconNodeStatusGauge.Record(ctx, 0,
			metric.WithAttributes(semconv.ServerAddress(serverAddr)),
			metric.WithAttributes(beaconClientStatusAttribute(status)),
		)
	}
}

func beaconClientStatusAttribute(value beaconNodeStatus) attribute.KeyValue {
	eventNameAttrName := fmt.Sprintf("%s.sync.status", observabilityNamespace)
	return attribute.String(eventNameAttrName, string(value))
}

func recordAttestationDataClientSelection(ctx context.Context, clientAddr string) {
	attr := []attribute.KeyValue{
		semconv.ServerAddress(clientAddr),
	}

	attestationDataClientSelections.Add(ctx, 1, metric.WithAttributes(attr...))
}
