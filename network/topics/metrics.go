package topics

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"go.uber.org/zap"
)

// TODO: replace with new metrics
var (
	metricPubsubTrace = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "ssv:network:pubsub:trace",
		Help: "Traces of pubsub messages",
	}, []string{"type"})
	metricPubsubOutbound = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "ssv:p2p:pubsub:msg:out",
		Help: "Count broadcasted messages",
	}, []string{"topic"})
	metricPubsubInbound = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "ssv:p2p:pubsub:msg:in",
		Help: "Count incoming messages",
	}, []string{"topic", "msg_type"})
	metricPubsubPeerScoreInspect = promauto.NewGaugeVec(prometheus.GaugeOpts{
		Name: "ssv:p2p:pubsub:score:inspect",
		Help: "Gauge for negative peer scores",
	}, []string{"pid"})
)

func init() {
	logger := zap.L()
	if err := prometheus.Register(metricPubsubTrace); err != nil {
		logger.Debug("could not register prometheus collector")
	}
	if err := prometheus.Register(metricPubsubOutbound); err != nil {
		logger.Debug("could not register prometheus collector")
	}
	if err := prometheus.Register(metricPubsubInbound); err != nil {
		logger.Debug("could not register prometheus collector")
	}
	if err := prometheus.Register(metricPubsubPeerScoreInspect); err != nil {
		logger.Debug("could not register prometheus collector")
	}
}
