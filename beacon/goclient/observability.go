package goclient

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/attestantio/go-eth2-client/spec/phase0"
	"github.com/ethereum/go-ethereum/rpc"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
	semconv "go.opentelemetry.io/otel/semconv/v1.26.0"

	"github.com/ssvlabs/ssv/observability"
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

func recordRequestDuration(ctx context.Context, routeName, serverAddr, requestMethod string, duration time.Duration, err error) {
	attr := []attribute.KeyValue{
		semconv.ServerAddress(serverAddr),
		semconv.HTTPRequestMethodKey.String(requestMethod),
		attribute.String("http.route_name", routeName),
	}

	var rpcErr rpc.Error
	if errors.As(err, &rpcErr) {
		attr = append(attr, attribute.Int("http.response.error_status_code", rpcErr.ErrorCode()))
	}

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
