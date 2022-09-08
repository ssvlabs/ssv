package topics

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"log"
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
	}, []string{"topic"})
	metricPubsubActiveMsgValidation = promauto.NewGaugeVec(prometheus.GaugeOpts{
		Name: "ssv:p2p:pubsub:msg:val:active",
		Help: "Count active message validation",
	}, []string{"topic"})
	metricPubsubPeerScoreInspect = promauto.NewGaugeVec(prometheus.GaugeOpts{
		Name: "ssv:p2p:pubsub:score:inspect",
		Help: "Gauge for negative peer scores",
	}, []string{"pid"})
	metricPubsubPeerScoreAverage = promauto.NewGauge(prometheus.GaugeOpts{
		Name: "ssv:p2p:pubsub:score:avg",
		Help: "Counts average score among peers",
	})
	metricPubsubPeerScorePositive = promauto.NewGauge(prometheus.GaugeOpts{
		Name: "ssv:p2p:pubsub:score:positive",
		Help: "Counts peers with positive score",
	})
	metricPubsubPeerScoreNegative = promauto.NewGauge(prometheus.GaugeOpts{
		Name: "ssv:p2p:pubsub:score:negative",
		Help: "Counts peers with negative score",
	})
)

func init() {
	if err := prometheus.Register(metricPubsubTrace); err != nil {
		log.Println("could not register prometheus collector")
	}
	if err := prometheus.Register(metricPubsubMsgValidationResults); err != nil {
		log.Println("could not register prometheus collector")
	}
	if err := prometheus.Register(metricPubsubOutbound); err != nil {
		log.Println("could not register prometheus collector")
	}
	if err := prometheus.Register(metricPubsubInbound); err != nil {
		log.Println("could not register prometheus collector")
	}
	if err := prometheus.Register(metricPubsubActiveMsgValidation); err != nil {
		log.Println("could not register prometheus collector")
	}
	if err := prometheus.Register(metricPubsubPeerScoreInspect); err != nil {
		log.Println("could not register prometheus collector")
	}
	if err := prometheus.Register(metricPubsubPeerScoreAverage); err != nil {
		log.Println("could not register prometheus collector")
	}
	if err := prometheus.Register(metricPubsubPeerScorePositive); err != nil {
		log.Println("could not register prometheus collector")
	}
	if err := prometheus.Register(metricPubsubPeerScoreNegative); err != nil {
		log.Println("could not register prometheus collector")
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
