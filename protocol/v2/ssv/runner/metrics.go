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
	}, []string{"pubKey"})
	metricsPreConsensusDuration = promauto.NewHistogramVec(prometheus.HistogramOpts{
		Name:    "ssv_validator_pre_consensus_duration_seconds",
		Help:    "Pre-consensus duration (seconds)",
		Buckets: []float64{0.5, 1, 2, 3, 4, 10},
	}, []string{"pubKey"})
	metricsPostConsensusDuration = promauto.NewHistogramVec(prometheus.HistogramOpts{
		Name:    "ssv_validator_post_consensus_duration_seconds",
		Help:    "Post-consensus duration (seconds)",
		Buckets: []float64{0.5, 1, 2, 3, 4, 10},
	}, []string{"pubKey"})
	metricsAttestationSubmissionDuration = promauto.NewHistogramVec(prometheus.HistogramOpts{
		Name:    "ssv_validator_attestation_submission_duration_seconds",
		Help:    "Attestation duration (seconds)",
		Buckets: []float64{0.02, 0.05, 0.1, 0.2, 0.5, 1, 5},
	}, []string{"pubKey"})
	metricsAttestationFullFlowDuration = promauto.NewHistogramVec(prometheus.HistogramOpts{
		Name:    "ssv_validator_attestation_full_flow_duration_seconds",
		Help:    "Attestation full flow duration (seconds)",
		Buckets: []float64{0.5, 1, 2, 3, 4, 10},
	}, []string{"pubKey"})
)

func init() {
	metricsList := []prometheus.Collector{
		metricsConsensusDuration,
		metricsPreConsensusDuration,
		metricsPostConsensusDuration,
		metricsAttestationSubmissionDuration,
		metricsAttestationFullFlowDuration,
	}

	for _, metric := range metricsList {
		if err := prometheus.Register(metric); err != nil {
			log.Println("could not register prometheus collector")
		}
	}
}
