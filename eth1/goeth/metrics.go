package goeth

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"log"
)

var (
	metricSyncEventsCountSuccess = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "ssv:eth1:sync:events:validator:count:success",
		Help: "Count succeeded eth1 sync events",
	}, []string{"etype", "self"})
	metricSyncEventsCountFailed = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "ssv:eth1:sync:events:validator:count:failed",
		Help: "Count failed eth1 sync events",
	}, []string{"etype", "self"})
)

func init() {
	if err := prometheus.Register(metricSyncEventsCountSuccess); err != nil {
		log.Println("could not register prometheus collector")
	}
	if err := prometheus.Register(metricSyncEventsCountSuccess); err != nil {
		log.Println("could not register prometheus collector")
	}
}

func reportSyncEvent(eventType string, isOperatorEvent bool, err error) {
	self := "0"
	if isOperatorEvent {
		self = "1"
	}
	if err != nil {
		metricSyncEventsCountFailed.WithLabelValues(eventType, self).Inc()
		return
	}
	metricSyncEventsCountSuccess.WithLabelValues(eventType, self).Inc()
}
