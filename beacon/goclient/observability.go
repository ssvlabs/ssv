package goclient

import (
	"context"
	"fmt"
	"time"

	"github.com/ssvlabs/ssv/observability"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
	semconv "go.opentelemetry.io/otel/semconv/v1.26.0"
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

	requestDurationHistogram = observability.NewMetric(
		meter.Float64Histogram(
			metricName("request.duration"),
			metric.WithUnit("s"),
			metric.WithDescription("beacon data request duration in seconds"),
			metric.WithExplicitBucketBoundaries(observability.SecondsHistogramBuckets...)))

	beaconNodeStatusGauge = observability.NewMetric(
		meter.Int64Gauge(
			metricName("sync.status"),
			metric.WithDescription("beacon node status")))

	syncingDistanceGauge = observability.NewMetric(
		meter.Int64Gauge(
			metricName("sync.distance"),
			metric.WithUnit("{block}"),
			metric.WithDescription("consensus client syncing distance which is a delta between highest and current blocks")))
)

func metricName(name string) string {
	return fmt.Sprintf("%s.%s", observabilityNamespace, name)
}

func recordRequestDuration(ctx context.Context, routeName, serverAddr, requestMethod string, duration time.Duration) {
	requestDurationHistogram.Record(
		ctx,
		duration.Seconds(),
		metric.WithAttributes(
			semconv.ServerAddress(serverAddr),
			semconv.HTTPRequestMethodKey.String(requestMethod),
			attribute.String("http.route_name", routeName),
		))
}

func recordBeaconClientStatus(ctx context.Context, status beaconNodeStatus, nodeAddr string) {
	resetBeaconClientStatusGauge(ctx, nodeAddr)

	beaconNodeStatusGauge.Record(ctx, 1,
		metric.WithAttributes(beaconClientAddrAttribute(nodeAddr)),
		metric.WithAttributes(beaconClientStatusAttribute(status)),
	)
}

func resetBeaconClientStatusGauge(ctx context.Context, nodeAddr string) {
	for _, status := range []beaconNodeStatus{statusSynced, statusSyncing, statusUnknown} {
		beaconNodeStatusGauge.Record(ctx, 0,
			metric.WithAttributes(beaconClientAddrAttribute(nodeAddr)),
			metric.WithAttributes(beaconClientStatusAttribute(status)),
		)
	}
}

func beaconClientStatusAttribute(value beaconNodeStatus) attribute.KeyValue {
	eventNameAttrName := fmt.Sprintf("%s.sync.status", observabilityNamespace)
	return attribute.String(eventNameAttrName, string(value))
}

func beaconClientAddrAttribute(value string) attribute.KeyValue {
	eventNameAttrName := fmt.Sprintf("%s.addr", observabilityNamespace)
	return attribute.String(eventNameAttrName, value)
}
