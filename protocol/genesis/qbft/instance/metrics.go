package instance

import (
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	genesisspecqbft "github.com/ssvlabs/ssv-spec-pre-cc/qbft"
	genesisspectypes "github.com/ssvlabs/ssv-spec-pre-cc/types"
	"go.uber.org/zap"
)

var (
	metricsStageDurationGenesis = promauto.NewHistogramVec(prometheus.HistogramOpts{
		Name:    "ssv_validator_instance_stage_duration_seconds_genesis",
		Help:    "Instance stage duration (seconds)",
		Buckets: []float64{0.02, 0.05, 0.1, 0.2, 0.5, 1, 1.5, 2, 5},
	}, []string{"stage"})
	metricsRoundGenesis = promauto.NewGaugeVec(prometheus.GaugeOpts{
		Name: "ssv_qbft_instance_round_genesis",
		Help: "QBFT instance round",
	}, []string{"roleType"})
)

func init() {
	allMetrics := []prometheus.Collector{
		metricsStageDurationGenesis,
		metricsRoundGenesis,
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

func newMetrics(msgID genesisspectypes.MessageID) *metrics {
	return &metrics{
		proposalDuration: metricsStageDurationGenesis.WithLabelValues("proposal"),
		prepareDuration:  metricsStageDurationGenesis.WithLabelValues("prepare"),
		commitDuration:   metricsStageDurationGenesis.WithLabelValues("commit"),
		round:            metricsRoundGenesis.WithLabelValues(msgID.GetRoleType().String()),
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

func (m *metrics) SetRound(round genesisspecqbft.Round) {
	m.round.Set(float64(round))
}
