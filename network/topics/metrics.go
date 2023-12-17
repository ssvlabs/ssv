package topics

import (
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"go.uber.org/zap"
)

type Metrics interface {
	PeerScore(peer.ID, float64)
	PeerP4Score(peer.ID, float64)
}

// TODO: replace with new metrics
var (
	metricPubsubTrace = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "ssv:network:pubsub:trace",
		Help: "Traces of pubsub messages",
	}, []string{"type"})
	metricPubsubOutbound = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "ssv:p2p:pubsub:msg:out",
		Help: "Count broadcasted messages",
	}, []string{"topic"})
	metricPubsubInbound = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "ssv:p2p:pubsub:msg:in",
		Help: "Count incoming messages",
	}, []string{"topic", "msg_type"})
)

func init() {
	logger := zap.L()

	allMetrics := []prometheus.Collector{
		metricPubsubTrace,
		metricPubsubOutbound,
		metricPubsubInbound,
	}

	for i, c := range allMetrics {
		if err := prometheus.Register(c); err != nil {
			// TODO: think how to print metric name
			logger.Debug("could not register prometheus collector",
				zap.Int("index", i),
				zap.Error(err),
			)
		}
	}

}
