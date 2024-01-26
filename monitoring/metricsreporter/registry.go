package metricsreporter

import "github.com/prometheus/client_golang/prometheus"

func NewMetricsRegistry() *prometheus.Registry {
	reg := prometheus.NewRegistry()

	return reg
}
