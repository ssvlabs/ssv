package api

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

var (
	metricStreamOutboundCount = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "ssv:exporter:stream_outbound",
		Help: "count the outbound messages on stream channel",
	}, []string{"cid"})
	metricStreamOutboundErrorsCount = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "ssv:exporter:stream_outbound_errors",
		Help: "count the outbound messages failures on stream channel",
	}, []string{"cid"})
	metricHandleParticipantsQueryReq = promauto.NewHistogramVec(prometheus.HistogramOpts{
		Name: "ssv:exporter:handle_participants_query_req",
		Help: "Histogram of HTTP request durations in seconds.",
	}, []string{"role"})
)

func reportStreamOutbound(cid string, err error) {
	if err != nil {
		metricStreamOutboundErrorsCount.WithLabelValues(cid).Inc()
	} else {
		metricStreamOutboundCount.WithLabelValues(cid).Inc()
	}
}
