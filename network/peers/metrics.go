package peers

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"log"
)

var (
	metricsStreams = promauto.NewGauge(prometheus.GaugeOpts{
		Name: "ssv:network:stream",
		Help: "Counts opened/closed streams",
	})
	metricsConnections = promauto.NewGauge(prometheus.GaugeOpts{
		Name: "ssv:network:connections",
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
}
