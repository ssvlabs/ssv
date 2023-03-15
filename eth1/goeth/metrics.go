package goeth

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"go.uber.org/zap"
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
	statusUnknown eth1NodeStatus = 0
	statusSyncing eth1NodeStatus = 1
	statusOK      eth1NodeStatus = 2
)

func init() {
	if err := prometheus.Register(metricsEth1NodeStatus); err != nil {
		zap.L().Debug("could not register prometheus collector", zap.Error(err))
	}
	if err := prometheus.Register(metricSyncEventsCountSuccess); err != nil {
		zap.L().Debug("could not register prometheus collector", zap.Error(err))
	}
	if err := prometheus.Register(metricSyncEventsCountSuccess); err != nil {
		zap.L().Debug("could not register prometheus collector", zap.Error(err))
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
