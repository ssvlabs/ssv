package streams

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"log"
)

var (
	metricsStreamRequests = promauto.NewGauge(prometheus.GaugeOpts{
		Name: "ssv:p2p:streams:req:count",
		Help: "Count requests made via streams",
	})
	metricsStreamRequestsActive = promauto.NewGauge(prometheus.GaugeOpts{
		Name: "ssv:p2p:streams:req:active",
		Help: "Count requests made via streams",
	})
	metricsStreamRequestsSuccess = promauto.NewCounter(prometheus.CounterOpts{
		Name: "ssv:p2p:streams:req:success",
		Help: "Count successful requests made via streams",
	})
	metricsStreamResponses = promauto.NewCounter(prometheus.CounterOpts{
		Name: "ssv:p2p:streams:res",
		Help: "Count responses for streams",
	})
)

func init() {
	if err := prometheus.Register(metricsStreamRequestsActive); err != nil {
		log.Println("could not register prometheus collector")
	}
	if err := prometheus.Register(metricsStreamRequestsSuccess); err != nil {
		log.Println("could not register prometheus collector")
	}
	if err := prometheus.Register(metricsStreamResponses); err != nil {
		log.Println("could not register prometheus collector")
	}
}
