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
		log.Println("could not register prometheus collector")
	}
	if err := prometheus.Register(metricRejectedNodes); err != nil {
		log.Println("could not register prometheus collector")
	}
	if err := prometheus.Register(metricPublishEnrPings); err != nil {
		log.Println("could not register prometheus collector")
	}
	if err := prometheus.Register(metricPublishEnrPongs); err != nil {
		log.Println("could not register prometheus collector")
	}
}
