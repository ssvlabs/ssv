package discovery

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"log"
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
)

func init() {
	if err := prometheus.Register(metricFoundNodes); err != nil {
		log.Println("could not register prometheus collector")
	}
	if err := prometheus.Register(metricRejectedNodes); err != nil {
		log.Println("could not register prometheus collector")
	}
}
