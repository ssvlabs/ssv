package metrics

import (
	"go.uber.org/zap"
	"time"

	specqbft "github.com/bloxapp/ssv-spec/qbft"
	spectypes "github.com/bloxapp/ssv-spec/types"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

var (
	metricsConsensusDuration = promauto.NewHistogramVec(prometheus.HistogramOpts{
		Name:    "ssv_validator_consensus_duration_seconds",
		Help:    "Consensus duration (seconds)",
		Buckets: []float64{0.02, 0.05, 0.1, 0.2, 0.5, 1, 5},
	}, []string{"role"})
	metricsPreConsensusDuration = promauto.NewHistogramVec(prometheus.HistogramOpts{
		Name:    "ssv_validator_pre_consensus_duration_seconds",
		Help:    "Pre-consensus duration (seconds)",
		Buckets: []float64{0.02, 0.05, 0.1, 0.2, 0.5, 1, 5},
	}, []string{"role"})
	metricsPostConsensusDuration = promauto.NewHistogramVec(prometheus.HistogramOpts{
		Name:    "ssv_validator_post_consensus_duration_seconds",
		Help:    "Post-consensus duration (seconds)",
		Buckets: []float64{0.02, 0.05, 0.1, 0.2, 0.5, 1, 5},
	}, []string{"role"})
	metricsBeaconSubmissionDuration = promauto.NewHistogramVec(prometheus.HistogramOpts{
		Name:    "ssv_validator_beacon_submission_duration_seconds",
		Help:    "Submission to beacon node duration (seconds)",
		Buckets: []float64{0.02, 0.05, 0.1, 0.2, 0.5, 1, 5},
	}, []string{"role"})
	metricsDutyFullFlowDuration = promauto.NewHistogramVec(prometheus.HistogramOpts{
		Name:    "ssv_validator_duty_full_flow_duration_seconds",
		Help:    "Duty full flow duration (seconds)",
		Buckets: []float64{0.02, 0.05, 0.1, 0.2, 0.5, 1, 5},
	}, []string{"role"})
	metricsDutyFullFlowFirstRoundDuration = promauto.NewHistogramVec(prometheus.HistogramOpts{
		Name: "ssv_validator_duty_full_flow_first_round_duration_seconds",
		Help: "Duty full flow at first round duration (seconds)",
		Buckets: []float64{
			0.1, 0.2, 0.3, 0.4, 0.5, 0.6, 0.7, 0.8, 0.9, 1.0,
			1.1, 1.2, 1.3, 1.4, 1.5, 1.6, 1.7, 1.8, 1.9, 2.0,
			2.1, 2.2, 2.3, 2.4, 2.5, 2.6, 2.7, 2.8, 2.9, 3.0,
			3.1, 3.2, 3.3, 3.4, 3.5, 3.6, 3.7, 3.8, 3.9, 4.0,
			4.1, 4.2, 4.3, 4.4, 4.5, 4.6, 4.7, 4.8, 4.9, 5.0,
		},
	}, []string{"role"})
	metricsRolesSubmitted = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "ssv_validator_roles_submitted",
		Help: "Submitted roles",
	}, []string{"role"})
	metricsRolesSubmissionFailures = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "ssv_validator_roles_failed",
		Help: "Submitted roles",
	}, []string{"role"})
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
	logger := zap.L()
	for _, metric := range metricsList {
		if err := prometheus.Register(metric); err != nil {
			logger.Debug("could not register prometheus collector")
		}
	}
}

// ConsensusMetrics defines metrics for consensus process.
type ConsensusMetrics struct {
	preConsensus                   prometheus.Observer
	consensus                      prometheus.Observer
	postConsensus                  prometheus.Observer
	beaconSubmission               prometheus.Observer
	dutyFullFlow                   prometheus.Observer
	dutyFullFlowFirstRound         prometheus.Observer
	rolesSubmitted                 prometheus.Counter
	rolesSubmissionFailures        prometheus.Counter
	preConsensusStart              time.Time
	consensusStart                 time.Time
	postConsensusStart             time.Time
	dutyFullFlowStart              time.Time
	dutyFullFlowCumulativeDuration time.Duration
}

func NewConsensusMetrics(role spectypes.BeaconRole) ConsensusMetrics {
	values := []string{role.String()}
	return ConsensusMetrics{
		preConsensus:            metricsPreConsensusDuration.WithLabelValues(values...),
		consensus:               metricsConsensusDuration.WithLabelValues(values...),
		postConsensus:           metricsPostConsensusDuration.WithLabelValues(values...),
		beaconSubmission:        metricsBeaconSubmissionDuration.WithLabelValues(values...),
		dutyFullFlow:            metricsDutyFullFlowDuration.WithLabelValues(values...),
		dutyFullFlowFirstRound:  metricsDutyFullFlowFirstRoundDuration.WithLabelValues(values...),
		rolesSubmitted:          metricsRolesSubmitted.WithLabelValues(values...),
		rolesSubmissionFailures: metricsRolesSubmissionFailures.WithLabelValues(values...),
	}
}

// StartPreConsensus stores pre-consensus start time.
func (cm *ConsensusMetrics) StartPreConsensus() {
	if cm != nil {
		cm.preConsensusStart = time.Now()
	}
}

// EndPreConsensus sends metrics for pre-consensus duration.
func (cm *ConsensusMetrics) EndPreConsensus() {
	if cm != nil && cm.preConsensus != nil && !cm.preConsensusStart.IsZero() {
		cm.preConsensus.Observe(time.Since(cm.preConsensusStart).Seconds())
		cm.preConsensusStart = time.Time{}
	}
}

// StartConsensus stores consensus start time.
func (cm *ConsensusMetrics) StartConsensus() {
	if cm != nil {
		cm.consensusStart = time.Now()
	}
}

// EndConsensus sends metrics for consensus duration.
func (cm *ConsensusMetrics) EndConsensus() {
	if cm != nil && cm.consensus != nil && !cm.consensusStart.IsZero() {
		cm.consensus.Observe(time.Since(cm.consensusStart).Seconds())
		cm.consensusStart = time.Time{}
	}
}

// StartPostConsensus stores post-consensus start time.
func (cm *ConsensusMetrics) StartPostConsensus() {
	if cm != nil {
		cm.postConsensusStart = time.Now()
	}
}

// EndPostConsensus sends metrics for post-consensus duration.
func (cm *ConsensusMetrics) EndPostConsensus() {
	if cm != nil && cm.postConsensus != nil && !cm.postConsensusStart.IsZero() {
		cm.postConsensus.Observe(time.Since(cm.postConsensusStart).Seconds())
		cm.postConsensusStart = time.Time{}
	}
}

// StartDutyFullFlow stores duty full flow start time.
func (cm *ConsensusMetrics) StartDutyFullFlow() {
	if cm != nil {
		cm.dutyFullFlowStart = time.Now()
		cm.dutyFullFlowCumulativeDuration = 0
	}
}

// PauseDutyFullFlow stores duty full flow cumulative duration with ability to continue the flow.
func (cm *ConsensusMetrics) PauseDutyFullFlow() {
	if cm != nil {
		cm.dutyFullFlowCumulativeDuration += time.Since(cm.dutyFullFlowStart)
		cm.dutyFullFlowStart = time.Time{}
	}
}

// ContinueDutyFullFlow continues measuring duty full flow duration.
func (cm *ConsensusMetrics) ContinueDutyFullFlow() {
	if cm != nil {
		cm.dutyFullFlowStart = time.Now()
	}
}

// EndDutyFullFlow sends metrics for duty full flow duration.
func (cm *ConsensusMetrics) EndDutyFullFlow(round specqbft.Round) {
	if cm != nil && cm.dutyFullFlow != nil && !cm.dutyFullFlowStart.IsZero() {
		cm.dutyFullFlowCumulativeDuration += time.Since(cm.dutyFullFlowStart)
		cm.dutyFullFlow.Observe(cm.dutyFullFlowCumulativeDuration.Seconds())

		if round == 1 {
			cm.dutyFullFlowFirstRound.Observe(cm.dutyFullFlowCumulativeDuration.Seconds())
		}

		cm.dutyFullFlowStart = time.Time{}
		cm.dutyFullFlowCumulativeDuration = 0
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
