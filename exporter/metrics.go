package exporter

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"log"
)

var (
	metricOperatorIndex = promauto.NewGaugeVec(prometheus.GaugeOpts{
		Name: "ssv:exporter:operator_index",
		Help: "operator footprint",
	}, []string{"pubKey", "name"})
)

func init() {
	if err := prometheus.Register(metricOperatorIndex); err != nil {
		log.Println("could not register prometheus collector")
	}
}
