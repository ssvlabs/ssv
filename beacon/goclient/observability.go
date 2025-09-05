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
	semconv "go.opentelemetry.io/otel/semconv/v1.26.0"
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
)

func recordSingleClientRequest(
	ctx context.Context,
	logger *zap.Logger,
	routeName, clientAddr, httpMethod string,
	duration time.Duration,
	err error,
) {
	// Log the request, but only if it has errored or if it took long enough that we want to pay attention
	// to it (there are too many requests being made to log them every time, hence this limiting).
	if err != nil || duration > 1*time.Millisecond {
		logger.Debug("CL single-client request done",
			zap.String("client_addr", clientAddr),
			zap.String("route_name", routeName),
			zap.String("http_method", httpMethod),
			zap.Bool("success", err == nil),
			zap.Error(err),
			fields.Took(duration),
		)
	}

	// Build metric attributes, add error-code attribute in case there is an error.
	attr := []attribute.KeyValue{
		semconv.ServerAddress(clientAddr),
		semconv.HTTPRequestMethodKey.String(httpMethod),
		attribute.String("http.route_name", routeName),
	}
	if err != nil {
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

func recordMultiClientRequest(
	ctx context.Context,
	logger *zap.Logger,
	routeName, httpMethod string,
	duration time.Duration,
	err error,
) {
	// Log the request, but only if it has errored or if it took long enough that we want to pay attention
	// to it (there are too many requests being made to log them every time, hence this limiting).
	if err != nil || duration > 1*time.Millisecond {
		logger.Debug("CL multi-client request done",
			zap.String("route_name", routeName),
			zap.String("http_method", httpMethod),
			zap.Bool("success", err == nil),
			zap.Error(err),
			fields.Took(duration),
		)
	}

	// Build metric attributes, add error-code attribute in case there is an error.
	attr := []attribute.KeyValue{
		semconv.ServerAddress("multi-client"),
		semconv.HTTPRequestMethodKey.String(httpMethod),
		attribute.String("http.route_name", routeName),
	}
	if err != nil {
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
