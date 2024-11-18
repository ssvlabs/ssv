package goclient

import (
	"context"
	"fmt"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"

	"github.com/ssvlabs/ssv/observability"
)

type beaconNodeStatus string

const (
	observabilityComponentName      = "github.com/ssvlabs/ssv/beacon/goclient"
	observabilityComponentNamespace = "ssv.cl"

	statusUnknown beaconNodeStatus = "unknown"
	statusSyncing beaconNodeStatus = "syncing"
	statusOK      beaconNodeStatus = "ok"
)

var (
	meter = otel.Meter(observabilityComponentName)

	attestationDataRequestHistogram = observability.GetMetric(
		fmt.Sprintf("%s.attestation_data_request.duration", observabilityComponentNamespace),
		func(metricName string) (metric.Float64Histogram, error) {
			return meter.Float64Histogram(
				metricName,
				metric.WithUnit("s"),
				metric.WithDescription("beacon data request duration in seconds"),
				metric.WithExplicitBucketBoundaries(observability.SecondsHistogramBuckets...))
		},
	)

	beaconNodeStatusGauge = observability.GetMetric(
		fmt.Sprintf("%s.syncing.status", observabilityComponentNamespace),
		func(metricName string) (metric.Int64Gauge, error) {
			return meter.Int64Gauge(
				metricName,
				metric.WithDescription("beacon node status"))
		},
	)

	syncingDistanceGauge = observability.GetMetric(
		fmt.Sprintf("%s.syncing.distance", observabilityComponentNamespace),
		func(metricName string) (metric.Int64Gauge, error) {
			return meter.Int64Gauge(
				metricName,
				metric.WithUnit("{block}"),
				metric.WithDescription("consensus client syncing distance which is a delta between highest and current blocks"))
		},
	)
)

func recordBeaconClientStatus(ctx context.Context, status beaconNodeStatus, nodeAddr string) {
	resetBeaconClientStatusGauge(ctx, nodeAddr)

	beaconNodeStatusGauge.Record(ctx, 1,
		metric.WithAttributes(beaconClientAddrAttribute(nodeAddr)),
		metric.WithAttributes(beaconClientStatusAttribute(status)),
	)
}

func resetBeaconClientStatusGauge(ctx context.Context, nodeAddr string) {
	for _, status := range []beaconNodeStatus{statusOK, statusSyncing, statusUnknown} {
		beaconNodeStatusGauge.Record(ctx, 0,
			metric.WithAttributes(beaconClientAddrAttribute(nodeAddr)),
			metric.WithAttributes(beaconClientStatusAttribute(status)),
		)
	}
}

func beaconClientStatusAttribute(value beaconNodeStatus) attribute.KeyValue {
	eventNameAttrName := fmt.Sprintf("%s.status", observabilityComponentNamespace)
	return attribute.String(eventNameAttrName, string(value))
}

func beaconClientAddrAttribute(value string) attribute.KeyValue {
	eventNameAttrName := fmt.Sprintf("%s.addr", observabilityComponentNamespace)
	return attribute.String(eventNameAttrName, value)
}
