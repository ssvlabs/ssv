package topics

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"go.uber.org/zap"
)

var (
	metricPubsubTrace = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "ssv:network:pubsub:trace",
		Help: "Traces of pubsub messages",
	}, []string{"type"})
	metricPubsubMsgValidationResults = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "ssv:network:pubsub:msg:validation",
		Help: "Traces of pubsub message validation results",
	}, []string{"type"})
	metricPubsubOutbound = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "ssv:p2p:pubsub:msg:out",
		Help: "Count broadcasted messages",
	}, []string{"topic"})
	metricPubsubInbound = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "ssv:p2p:pubsub:msg:in",
		Help: "Count incoming messages",
	}, []string{"topic", "msg_type"})
	metricPubsubActiveMsgValidation = promauto.NewGaugeVec(prometheus.GaugeOpts{
		Name: "ssv:p2p:pubsub:msg:val:active",
		Help: "Count active message validation",
	}, []string{"topic"})
	metricPubsubPeerScoreInspect = promauto.NewGaugeVec(prometheus.GaugeOpts{
		Name: "ssv:p2p:pubsub:score:inspect",
		Help: "Gauge for negative peer scores",
	}, []string{"pid"})
)

func init() {
	if err := prometheus.Register(metricPubsubTrace); err != nil {
		zap.L().Debug("could not register prometheus collector", zap.Error(err))
	}
	if err := prometheus.Register(metricPubsubMsgValidationResults); err != nil {
		zap.L().Debug("could not register prometheus collector", zap.Error(err))
	}
	if err := prometheus.Register(metricPubsubOutbound); err != nil {
		zap.L().Debug("could not register prometheus collector", zap.Error(err))
	}
	if err := prometheus.Register(metricPubsubInbound); err != nil {
		zap.L().Debug("could not register prometheus collector", zap.Error(err))
	}
	if err := prometheus.Register(metricPubsubActiveMsgValidation); err != nil {
		zap.L().Debug("could not register prometheus collector", zap.Error(err))
	}
	if err := prometheus.Register(metricPubsubPeerScoreInspect); err != nil {
		zap.L().Debug("could not register prometheus collector", zap.Error(err))
	}
}

type msgValidationResult string

var (
	validationResultNoData   msgValidationResult = "no_data"
	validationResultEncoding msgValidationResult = "encoding"
)

func reportValidationResult(result msgValidationResult) {
	metricPubsubMsgValidationResults.WithLabelValues(string(result)).Inc()
}
