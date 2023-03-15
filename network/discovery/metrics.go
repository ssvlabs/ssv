package discovery

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"go.uber.org/zap"
)

var (
	metricRejectedNodes = promauto.NewCounter(prometheus.CounterOpts{
		Name: "ssv:network:discovery:rejected",
		Help: "Counts nodes that were found with discovery but rejected",
	})
	metricFoundNodes = promauto.NewCounter(prometheus.CounterOpts{
		Name: "ssv:network:discovery:found",
		Help: "Counts nodes that were found with discovery",
	})
	metricPublishEnrPings = promauto.NewCounter(prometheus.CounterOpts{
		Name: "ssv:network:discovery:enr_ping",
		Help: "Counts the number of ping requests we made as part of ENR publishing",
	})
	metricPublishEnrPongs = promauto.NewCounter(prometheus.CounterOpts{
		Name: "ssv:network:discovery:enr_pong",
		Help: "Counts the number of pong responses we got as part of ENR publishing",
	})
)

func init() {
	if err := prometheus.Register(metricFoundNodes); err != nil {
		zap.L().Debug("could not register prometheus collector", zap.Error(err))
	}
	if err := prometheus.Register(metricRejectedNodes); err != nil {
		zap.L().Debug("could not register prometheus collector", zap.Error(err))
	}
	if err := prometheus.Register(metricPublishEnrPings); err != nil {
		zap.L().Debug("could not register prometheus collector", zap.Error(err))
	}
	if err := prometheus.Register(metricPublishEnrPongs); err != nil {
		zap.L().Debug("could not register prometheus collector", zap.Error(err))
	}
}
