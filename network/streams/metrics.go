package streams

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"go.uber.org/zap"
)

var (
	metricsStreamOutgoingRequests = promauto.NewGaugeVec(prometheus.GaugeOpts{
		Name: "ssv:p2p:streams:req:out",
		Help: "Count requests made via streams",
	}, []string{"pid"})
	metricsStreamRequestsActive = promauto.NewGaugeVec(prometheus.GaugeOpts{
		Name: "ssv:p2p:streams:req:active",
		Help: "Count requests made via streams",
	}, []string{"pid"})
	metricsStreamRequestsSuccess = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "ssv:p2p:streams:req:success",
		Help: "Count successful requests made via streams",
	}, []string{"pid"})
	metricsStreamResponses = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "ssv:p2p:streams:res",
		Help: "Count responses for streams",
	}, []string{"pid"})
	metricsStreamRequests = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "ssv:p2p:streams:req",
		Help: "Count responses for streams",
	}, []string{"pid"})
)

func init() {
	logger := zap.L()
	if err := prometheus.Register(metricsStreamOutgoingRequests); err != nil {
		logger.Debug("could not register prometheus collector")
	}
	if err := prometheus.Register(metricsStreamRequestsActive); err != nil {
		logger.Debug("could not register prometheus collector")
	}
	if err := prometheus.Register(metricsStreamRequestsSuccess); err != nil {
		logger.Debug("could not register prometheus collector")
	}
	if err := prometheus.Register(metricsStreamResponses); err != nil {
		logger.Debug("could not register prometheus collector")
	}
	if err := prometheus.Register(metricsStreamRequests); err != nil {
		logger.Debug("could not register prometheus collector")
	}
}
