package topics

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"log"
)

var (
	metricsPubsubTrace = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "ssv:network:pubsub:trace",
		Help: "Traces of pubsub messages",
	}, []string{"type"})
	metricsPubsubMsgValidationResults = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "ssv:network:pubsub:msg:validation",
		Help: "Traces of pubsub message validation results",
	}, []string{"type"})
	metricsPubsubOutbound = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "ssv:p2p:pubsub:msg:out",
		Help: "Count broadcasted messages",
	}, []string{"topic"})
	metricsPubsubInbound = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "ssv:p2p:pubsub:msg:in",
		Help: "Count incoming messages",
	}, []string{"topic"})
)

func init() {
	if err := prometheus.Register(metricsPubsubTrace); err != nil {
		log.Println("could not register prometheus collector")
	}
	if err := prometheus.Register(metricsPubsubMsgValidationResults); err != nil {
		log.Println("could not register prometheus collector")
	}
	if err := prometheus.Register(metricsPubsubOutbound); err != nil {
		log.Println("could not register prometheus collector")
	}
	if err := prometheus.Register(metricsPubsubInbound); err != nil {
		log.Println("could not register prometheus collector")
	}
}

type msgValidationResult string

var (
	validationResultNoData   msgValidationResult = "no_data"
	validationResultSelf     msgValidationResult = "self"
	validationResultEncoding msgValidationResult = "encoding"
	validationResultTopic    msgValidationResult = "topic"
	validationResultValid    msgValidationResult = "valid"
)

func reportValidationResult(result msgValidationResult) {
	metricsPubsubMsgValidationResults.WithLabelValues(string(result)).Inc()
}
