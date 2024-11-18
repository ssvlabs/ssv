package discovery

import (
	"fmt"

	"github.com/ssvlabs/ssv/observability"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/metric"
)

const (
	observabilityComponentName      = "github.com/ssvlabs/ssv/network/discovery"
	observabilityComponentNamespace = "ssv.p2p.discovery"
)

var (
	meter = otel.Meter(observabilityComponentName)

	peerDiscoveriesCounter = observability.GetMetric(
		fmt.Sprintf("%s.peers", observabilityComponentNamespace),
		func(metricName string) (metric.Int64Counter, error) {
			return meter.Int64Counter(
				metricName,
				metric.WithUnit("{peer}"),
				metric.WithDescription("total number of peers discovered"))
		},
	)

	peerRejectionsCounter = observability.GetMetric(
		fmt.Sprintf("%s.rejections", observabilityComponentNamespace),
		func(metricName string) (metric.Int64Counter, error) {
			return meter.Int64Counter(
				metricName,
				metric.WithUnit("{peer}"),
				metric.WithDescription("total number of peers rejected during discovery"))
		},
	)
)
