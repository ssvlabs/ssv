package observability

import (
	"go.opentelemetry.io/otel/exporters/prometheus"
	"go.opentelemetry.io/otel/sdk/resource"
)

type Config struct {
	metricsEnabled bool
}

type Dependencies struct {
	ResourceMerge func(*resource.Resource, *resource.Resource) (*resource.Resource, error)
	PrometheusNew func(opts ...prometheus.Option) (*prometheus.Exporter, error)
}

func defaultDependencies() Dependencies {
	return Dependencies{
		ResourceMerge: resource.Merge,
		PrometheusNew: prometheus.New,
	}
}
