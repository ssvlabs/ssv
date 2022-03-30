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
)

func init() {
	if err := prometheus.Register(metricsPubsubTrace); err != nil {
		log.Println("could not register prometheus collector")
	}
	if err := prometheus.Register(metricsPubsubMsgValidationResults); err != nil {
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
