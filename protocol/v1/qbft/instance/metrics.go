package instance

import (
	"encoding/hex"
	"log"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"

	"github.com/bloxapp/ssv/protocol/v1/message"
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
		Name:    "ssv:validator:instance_stage_duration_seconds",
		Help:    "Instance stage duration (seconds)",
		Buckets: []float64{0.02, 0.05, 0.1, 0.2, 0.5, 1, 1.5, 2, 5},
	}, []string{"stage", "pubKey"})
)

func init() {
	for _, c := range allMetrics {
		if err := prometheus.Register(c); err != nil {
			log.Println("could not register prometheus collector")
		}
	}
}

func (i *Instance) observeStageDurationMetric(stage string, value float64) {
	messageID := message.ToMessageID(i.GetState().GetIdentifier())
	metricsDurationStage.
		WithLabelValues(stage, hex.EncodeToString(messageID.GetPubKey())).
		Observe(value)
}
