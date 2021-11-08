package api

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

var (
	metricStreamOutboundQueueCount = promauto.NewGaugeVec(prometheus.GaugeOpts{
		Name: "ssv:exporter:stream_outbound_q_count",
		Help: "count the outbound messages on stream channel",
	}, []string{"cid"})
	metricStreamOutboundCount = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "ssv:exporter:stream_outbound",
		Help: "count the outbound messages on stream channel",
	}, []string{"cid"})
	metricStreamOutboundErrorsCount = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "ssv:exporter:stream_outbound_errors",
		Help: "count the outbound messages failures on stream channel",
	}, []string{"cid"})
)

func reportStreamOutbound(cid string, err error) {
	if err != nil {
		metricStreamOutboundErrorsCount.WithLabelValues(cid).Inc()
	} else {
		metricStreamOutboundCount.WithLabelValues(cid).Inc()
	}
}

func reportStreamOutboundQueueCount(cid string, inc bool) {
	if inc {
		metricStreamOutboundQueueCount.WithLabelValues(cid).Inc()
	} else {
		metricStreamOutboundQueueCount.WithLabelValues(cid).Dec()
	}
}
