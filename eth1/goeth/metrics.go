package goeth

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"log"
)

type eth1NodeStatus int32

var (
	metricSyncEventsCountSuccess = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "ssv:eth1:sync:count:success",
		Help: "Count succeeded eth1 sync events",
	}, []string{"etype", "self"})
	metricSyncEventsCountFailed = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "ssv:eth1:sync:count:failed",
		Help: "Count failed eth1 sync events",
	}, []string{"etype", "self"})
	metricsEth1NodeStatus = promauto.NewGauge(prometheus.GaugeOpts{
		Name: "ssv:eth1:node_status",
		Help: "Status of the connected eth1 node",
	})
	statusUnknown eth1NodeStatus = 0
	statusSyncing eth1NodeStatus = 1
	statusOK      eth1NodeStatus = 2
)

func init() {
	if err := prometheus.Register(metricsEth1NodeStatus); err != nil {
		log.Println("could not register prometheus collector")
	}
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

func reportNodeStatus(status eth1NodeStatus) {
	metricsEth1NodeStatus.Set(float64(status))
}
