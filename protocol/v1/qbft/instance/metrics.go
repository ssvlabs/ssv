package instance

import (
	"log"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

var (
	allMetrics = []prometheus.Collector{
		metricsIBFTStage,
		metricsIBFTRound,
		metricsDurationStage,
	}
	metricsIBFTStage = promauto.NewGaugeVec(prometheus.GaugeOpts{
		Name: "ssv:validator:ibft_stage",
		Help: "IBFTs stage",
	}, []string{"identifier", "pubKey"})
	metricsIBFTRound = promauto.NewGaugeVec(prometheus.GaugeOpts{
		Name: "ssv:validator:ibft_round",
		Help: "IBFTs round",
	}, []string{"identifier", "pubKey"})
	metricsDurationStage = promauto.NewHistogramVec(prometheus.HistogramOpts{
		Name:    "ssv:validator:duration_instance_stage",
		Help:    "Instance stage duration (seconds)",
		Buckets: []float64{0.01, 0.05, 0.1, 0.2, 0.5, 1, 2, 5, 10, 60},
	}, []string{"stage", "pubKey"})
)

func init() {
	for _, c := range allMetrics {
		if err := prometheus.Register(c); err != nil {
			log.Println("could not register prometheus collector")
		}
	}
}
