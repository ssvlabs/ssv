package connections

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"log"
)

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
	if err := prometheus.Register(metricsStreams); err != nil {
		log.Println("could not register prometheus collector")
	}
	if err := prometheus.Register(metricsConnections); err != nil {
		log.Println("could not register prometheus collector")
	}
	if err := prometheus.Register(metricsFilteredConnections); err != nil {
		log.Println("could not register prometheus collector")
	}
}
