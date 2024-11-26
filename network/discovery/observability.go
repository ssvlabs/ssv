package discovery

import (
	"fmt"

	"github.com/ssvlabs/ssv/observability"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/metric"
)

const (
	observabilityName      = "github.com/ssvlabs/ssv/network/discovery"
	observabilityNamespace = "ssv.p2p.discovery"
)

var (
	meter = otel.Meter(observabilityName)

	peerDiscoveriesCounter = observability.NewMetric(
		meter.Int64Counter(
			metricName("peers"),
			metric.WithUnit("{peer}"),
			metric.WithDescription("total number of peers discovered")))

	peerRejectionsCounter = observability.NewMetric(
		meter.Int64Counter(
			metricName("rejections"),
			metric.WithUnit("{peer}"),
			metric.WithDescription("total number of peers rejected during discovery")))
)

func metricName(name string) string {
	return fmt.Sprintf("%s.%s", observabilityNamespace, name)
}
