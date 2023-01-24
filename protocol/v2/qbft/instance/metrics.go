package instance

import (
	"encoding/hex"
	spectypes "github.com/bloxapp/ssv-spec/types"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"log"
)

var (
	metricsDurationStage = promauto.NewHistogramVec(prometheus.HistogramOpts{
		Name:    "ssv_validator_instance_stage_duration_seconds",
		Help:    "Instance stage duration (seconds)",
		Buckets: []float64{0.02, 0.05, 0.1, 0.2, 0.5, 1, 1.5, 2, 5},
	}, []string{"stage", "pubKey"})
	metricsQBFTInstanceRound = promauto.NewGaugeVec(prometheus.GaugeOpts{
		Name: "ssv_qbft_instance_round",
		Help: "QBFT instance round",
	}, []string{"roleType", "pubKey"})
)

func init() {
	allMetrics := []prometheus.Collector{
		metricsDurationStage,
		metricsQBFTInstanceRound,
	}
	for _, c := range allMetrics {
		if err := prometheus.Register(c); err != nil {
			log.Println("could not register prometheus collector")
		}
	}
}

func (i *Instance) observeStageDurationMetric(stage string, value float64) {
	messageID := spectypes.MessageIDFromBytes(i.State.ID)
	metricsDurationStage.
		WithLabelValues(stage, hex.EncodeToString(messageID.GetPubKey())).
		Observe(value)
}
