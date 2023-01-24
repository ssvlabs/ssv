package goeth

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"log"
)

type eth1NodeStatus int32

var (
	metricSyncEventsCountSuccess = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "ssv_eth1_sync_count_success",
		Help: "Count succeeded eth1 sync events",
	}, []string{"etype"})
	metricSyncEventsCountFailed = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "ssv_eth1_sync_count_failed",
		Help: "Count failed eth1 sync events",
	}, []string{"etype"})
	metricsEth1NodeStatus = promauto.NewGauge(prometheus.GaugeOpts{
		Name: "ssv_eth1_status",
		Help: "Status of the connected eth1 node",
	})
	metricsEth1LastSyncedBlock = promauto.NewGauge(prometheus.GaugeOpts{
		Name: "ssv_eth1_last_synced_block",
		Help: "ETH1 last synced block",
	})
	statusUnknown eth1NodeStatus = 0
	statusSyncing eth1NodeStatus = 1
	statusOK      eth1NodeStatus = 2
)

func init() {
	if err := prometheus.Register(metricsEth1NodeStatus); err != nil {
		log.Println("could not register prometheus collector")
	}
	if err := prometheus.Register(metricsEth1LastSyncedBlock); err != nil {
		log.Println("could not register prometheus collector")
	}
	if err := prometheus.Register(metricSyncEventsCountSuccess); err != nil {
		log.Println("could not register prometheus collector")
	}
	if err := prometheus.Register(metricSyncEventsCountSuccess); err != nil {
		log.Println("could not register prometheus collector")
	}
}

func reportSyncEvent(eventType string, err error) {
	if err != nil {
		metricSyncEventsCountFailed.WithLabelValues(eventType).Inc()
		return
	}
	metricSyncEventsCountSuccess.WithLabelValues(eventType).Inc()
}

func reportNodeStatus(status eth1NodeStatus) {
	metricsEth1NodeStatus.Set(float64(status))
}
