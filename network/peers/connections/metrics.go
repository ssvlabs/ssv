package connections

import (
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"go.uber.org/zap"
)

type Metrics interface {
	MessagesReceivedFromPeer(peer.ID)
	DeletePeerInfo(peerId peer.ID)
}

var (
	metricsStreams = promauto.NewGaugeVec(prometheus.GaugeOpts{
		Name: "ssv:network:stream",
		Help: "Counts opened/closed streams",
	}, []string{"protocol"})
	metricsConnections = promauto.NewGauge(prometheus.GaugeOpts{
		Name: "ssv:network:connections",
		Help: "Counts opened/closed connections",
	})
	metricsFilteredConnections = promauto.NewGauge(prometheus.GaugeOpts{
		Name: "ssv:network:connections:filtered",
		Help: "Counts opened/closed connections",
	})
)

func init() {
	logger := zap.L()
	if err := prometheus.Register(metricsStreams); err != nil {
		logger.Debug("could not register prometheus collector")
	}
	if err := prometheus.Register(metricsConnections); err != nil {
		logger.Debug("could not register prometheus collector")
	}
	if err := prometheus.Register(metricsFilteredConnections); err != nil {
		logger.Debug("could not register prometheus collector")
	}
}
