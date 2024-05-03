package instance

import (
	"time"

	specqbft "github.com/bloxapp/ssv-spec/qbft"
	spectypes "github.com/bloxapp/ssv-spec/types"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"go.uber.org/zap"
)

var (
	metricsStageDuration = promauto.NewHistogramVec(prometheus.HistogramOpts{
		Name:    "ssv_validator_instance_stage_duration_seconds",
		Help:    "Instance stage duration (seconds)",
		Buckets: []float64{0.02, 0.05, 0.1, 0.2, 0.5, 1, 1.5, 2, 5},
	}, []string{"stage"})
	metricsRound = promauto.NewGaugeVec(prometheus.GaugeOpts{
		Name: "ssv_qbft_instance_round",
		Help: "QBFT instance round",
	}, []string{"roleType"})
)

func init() {
	allMetrics := []prometheus.Collector{
		metricsStageDuration,
		metricsRound,
	}
	logger := zap.L()
	for _, c := range allMetrics {
		if err := prometheus.Register(c); err != nil {
			logger.Debug("could not register prometheus collector")
		}
	}
}

type metrics struct {
	StageStart       time.Time
	proposalDuration prometheus.Observer
	prepareDuration  prometheus.Observer
	commitDuration   prometheus.Observer
	round            prometheus.Gauge
}

func newMetrics(msgID spectypes.MessageID) *metrics {
	return &metrics{
		proposalDuration: metricsStageDuration.WithLabelValues("proposal"),
		prepareDuration:  metricsStageDuration.WithLabelValues("prepare"),
		commitDuration:   metricsStageDuration.WithLabelValues("commit"),
		round:            metricsRound.WithLabelValues(msgID.GetRoleType().String()),
	}
}

func (m *metrics) StartStage() {
	m.StageStart = time.Now()
}

func (m *metrics) EndStageProposal() {
	m.proposalDuration.Observe(time.Since(m.StageStart).Seconds())
	m.StageStart = time.Now()
}

func (m *metrics) EndStagePrepare() {
	m.prepareDuration.Observe(time.Since(m.StageStart).Seconds())
	m.StageStart = time.Now()
}

func (m *metrics) EndStageCommit() {
	m.commitDuration.Observe(time.Since(m.StageStart).Seconds())
	m.StageStart = time.Now()
}

func (m *metrics) SetRound(round specqbft.Round) {
	m.round.Set(float64(round))
}
