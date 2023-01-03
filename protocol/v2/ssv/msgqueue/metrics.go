package msgqueue

import (
	"log"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

var (
	metricsMsgQRatio = promauto.NewGaugeVec(prometheus.GaugeOpts{
		Name: "ssv:ibft:msgq:v2:ratio",
		Help: "The messages ratio between pop and add",
	}, []string{"identifier", "index_name", "msg_type", "consensus_type"})
	metricsMsgQProcessingDuration = promauto.NewHistogramVec(prometheus.HistogramOpts{
		Name:    "ssv:ibft:msgq:processing_duration_seconds",
		Help:    "Message processing duration (seconds)",
		Buckets: []float64{0.05, 0.1, 0.2, 0.5, 1.5},
	}, []string{"identifier", "index_name", "msg_type", "consensus_type"})
)

func init() {
	if err := prometheus.Register(metricsMsgQRatio); err != nil {
		log.Println("could not register prometheus collector")
	}
	if err := prometheus.Register(metricsMsgQProcessingDuration); err != nil {
		log.Println("could not register prometheus collector")
	}
}
