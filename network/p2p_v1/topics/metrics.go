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
	metricsBroadcastSuccess = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "ssv:network:broadcast:ok",
		Help: "Count successful broadcast-ed messages",
	}, []string{"topic"})
	metricsBroadcastFailed = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "ssv:network:broadcast:fail",
		Help: "Count failed broadcast-ed messages",
	}, []string{"topic"})
)

func init() {
	if err := prometheus.Register(metricsPubsubTrace); err != nil {
		log.Println("could not register prometheus collector")
	}
	if err := prometheus.Register(metricsPubsubMsgValidationResults); err != nil {
		log.Println("could not register prometheus collector")
	}
	if err := prometheus.Register(metricsBroadcastSuccess); err != nil {
		log.Println("could not register prometheus collector")
	}
	if err := prometheus.Register(metricsBroadcastFailed); err != nil {
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

func reportBroadcast(topic string, err error) {
	if err != nil {
		metricsBroadcastFailed.WithLabelValues(topic).Inc()
	} else {
		metricsBroadcastSuccess.WithLabelValues(topic).Inc()
	}
}
