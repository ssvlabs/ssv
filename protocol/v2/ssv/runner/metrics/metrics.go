package metrics

import (
	"encoding/hex"
	"log"
	"time"

	spectypes "github.com/bloxapp/ssv-spec/types"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
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
		Buckets: []float64{0.02, 0.05, 0.1, 0.2, 0.5, 1, 5},
	}, []string{"pubKey", "role"})
	metricsPostConsensusDuration = promauto.NewHistogramVec(prometheus.HistogramOpts{
		Name:    "ssv_validator_post_consensus_duration_seconds",
		Help:    "Post-consensus duration (seconds)",
		Buckets: []float64{0.02, 0.05, 0.1, 0.2, 0.5, 1, 5},
	}, []string{"pubKey", "role"})
	metricsBeaconSubmissionDuration = promauto.NewHistogramVec(prometheus.HistogramOpts{
		Name:    "ssv_validator_beacon_submission_duration_seconds",
		Help:    "Submission to beacon node duration (seconds)",
		Buckets: []float64{0.02, 0.05, 0.1, 0.2, 0.5, 1, 5},
	}, []string{"pubKey", "role"})
	metricsDutyFullFlowDuration = promauto.NewHistogramVec(prometheus.HistogramOpts{
		Name:    "ssv_validator_duty_full_flow_duration_seconds",
		Help:    "Duty full flow duration (seconds)",
		Buckets: []float64{0.5, 1, 2, 3, 4, 10},
	}, []string{"pubKey", "role"})
	metricsRolesSubmitted = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "ssv_validator_roles_submitted",
		Help: "Submitted roles",
	}, []string{"pubKey", "role"})
	metricsRolesSubmissionFailures = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "ssv_validator_roles_failed",
		Help: "Submitted roles",
	}, []string{"pubKey", "role"})
)

func init() {
	metricsList := []prometheus.Collector{
		metricsConsensusDuration,
		metricsPreConsensusDuration,
		metricsPostConsensusDuration,
		metricsBeaconSubmissionDuration,
		metricsDutyFullFlowDuration,
		metricsRolesSubmitted,
		metricsRolesSubmissionFailures,
	}

	for _, metric := range metricsList {
		if err := prometheus.Register(metric); err != nil {
			log.Println("could not register prometheus collector")
		}
	}
}

// ConsensusMetrics defines metrics for consensus process.
type ConsensusMetrics struct {
	preConsensus            prometheus.Observer
	consensus               prometheus.Observer
	postConsensus           prometheus.Observer
	beaconSubmission        prometheus.Observer
	dutyFullFlow            prometheus.Observer
	rolesSubmitted          prometheus.Counter
	rolesSubmissionFailures prometheus.Counter
	preConsensusStart       time.Time
	consensusStart          time.Time
	postConsensusStart      time.Time
}

func NewConsensusMetrics(pk []byte, role spectypes.BeaconRole) ConsensusMetrics {
	return ConsensusMetrics{
		preConsensus:            metricsPreConsensusDuration.WithLabelValues(hex.EncodeToString(pk), role.String()),
		consensus:               metricsConsensusDuration.WithLabelValues(hex.EncodeToString(pk), role.String()),
		postConsensus:           metricsPostConsensusDuration.WithLabelValues(hex.EncodeToString(pk), role.String()),
		beaconSubmission:        metricsBeaconSubmissionDuration.WithLabelValues(hex.EncodeToString(pk), role.String()),
		dutyFullFlow:            metricsDutyFullFlowDuration.WithLabelValues(hex.EncodeToString(pk), role.String()),
		rolesSubmitted:          metricsRolesSubmitted.WithLabelValues(hex.EncodeToString(pk), role.String()),
		rolesSubmissionFailures: metricsRolesSubmissionFailures.WithLabelValues(hex.EncodeToString(pk), role.String()),
	}
}

// StartPreConsensus stores pre-consensus start time.
func (cm *ConsensusMetrics) StartPreConsensus() {
	if cm != nil {
		cm.preConsensusStart = time.Now()
	}
}

// StartConsensus stores consensus start time.
func (cm *ConsensusMetrics) StartConsensus() {
	if cm != nil {
		cm.consensusStart = time.Now()
	}
}

// StartPostConsensus stores post-consensus start time.
func (cm *ConsensusMetrics) StartPostConsensus() {
	if cm != nil {
		cm.postConsensusStart = time.Now()
	}
}

// EndPreConsensus sends metrics for pre-consensus duration.
func (cm *ConsensusMetrics) EndPreConsensus() {
	if cm != nil && cm.preConsensus != nil && !cm.preConsensusStart.IsZero() {
		cm.preConsensus.Observe(time.Since(cm.preConsensusStart).Seconds())
	}
}

// EndConsensus sends metrics for consensus duration.
func (cm *ConsensusMetrics) EndConsensus() {
	if cm != nil && cm.consensus != nil && !cm.consensusStart.IsZero() {
		cm.consensus.Observe(time.Since(cm.consensusStart).Seconds())
	}
}

// EndPostConsensus sends metrics for post-consensus duration.
func (cm *ConsensusMetrics) EndPostConsensus() {
	if cm != nil && cm.postConsensus != nil && !cm.postConsensusStart.IsZero() {
		cm.postConsensus.Observe(time.Since(cm.postConsensusStart).Seconds())
	}
}

// StartBeaconSubmission returns a function that sends metrics for beacon submission duration.
func (cm *ConsensusMetrics) StartBeaconSubmission() (endBeaconSubmission func()) {
	if cm == nil || cm.beaconSubmission == nil {
		return func() {}
	}

	start := time.Now()
	return func() {
		cm.beaconSubmission.Observe(time.Since(start).Seconds())
	}
}

// EndDutyFullFlow sends metrics for duty full flow duration.
func (cm *ConsensusMetrics) EndDutyFullFlow() {
	if cm != nil && cm.dutyFullFlow != nil {
		start := cm.consensusStart
		if !cm.preConsensusStart.IsZero() {
			start = cm.preConsensusStart
		}
		if !start.IsZero() {
			cm.dutyFullFlow.Observe(time.Since(start).Seconds())
		}
	}
}

// RoleSubmitted increases submitted roles counter.
func (cm *ConsensusMetrics) RoleSubmitted() {
	if cm != nil && cm.rolesSubmitted != nil {
		cm.rolesSubmitted.Inc()
	}
}

// RoleSubmissionFailed increases non-submitted roles counter.
func (cm *ConsensusMetrics) RoleSubmissionFailed() {
	if cm != nil && cm.rolesSubmissionFailures != nil {
		cm.rolesSubmissionFailures.Inc()
	}
}
