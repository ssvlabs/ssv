package runner

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"log"
)

var (
	metricsConsensusDuration = promauto.NewHistogramVec(prometheus.HistogramOpts{
		Name:    "ssv_validator_consensus_duration_seconds",
		Help:    "Consensus duration (seconds)",
		Buckets: []float64{0.5, 1, 2, 3, 4, 10},
	}, []string{"pubKey", "role"})
	metricsPreConsensusDuration = promauto.NewHistogramVec(prometheus.HistogramOpts{
		Name:    "ssv_validator_pre_consensus_duration_seconds",
		Help:    "Pre-consensus duration (seconds)",
		Buckets: []float64{0.5, 1, 2, 3, 4, 10},
	}, []string{"pubKey", "role"})
	metricsPostConsensusDuration = promauto.NewHistogramVec(prometheus.HistogramOpts{
		Name:    "ssv_validator_post_consensus_duration_seconds",
		Help:    "Post-consensus duration (seconds)",
		Buckets: []float64{0.5, 1, 2, 3, 4, 10},
	}, []string{"pubKey", "role"})
	metricsBeaconSubmissionDuration = promauto.NewHistogramVec(prometheus.HistogramOpts{
		Name:    "ssv_validator_beacon_submission_duration_seconds",
		Help:    "Submission to beacon node duration (seconds)",
		Buckets: []float64{0.02, 0.05, 0.1, 0.2, 0.5, 1, 5},
	}, []string{"pubKey", "role"})
	metricsAttestationFullFlowDuration = promauto.NewHistogramVec(prometheus.HistogramOpts{
		Name:    "ssv_validator_attestation_full_flow_duration_seconds",
		Help:    "Attestation full flow duration (seconds)",
		Buckets: []float64{0.5, 1, 2, 3, 4, 10},
	}, []string{"pubKey", "role"})
)

func init() {
	metricsList := []prometheus.Collector{
		metricsConsensusDuration,
		metricsPreConsensusDuration,
		metricsPostConsensusDuration,
		metricsBeaconSubmissionDuration,
		metricsAttestationFullFlowDuration,
	}

	for _, metric := range metricsList {
		if err := prometheus.Register(metric); err != nil {
			log.Println("could not register prometheus collector")
		}
	}
}
