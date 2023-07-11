package metricsreporter

import (
	spectypes "github.com/bloxapp/ssv-spec/types"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"go.uber.org/zap"
)

// TODO: implement all methods

const (
	executionClientFailure = float64(0)
	executionClientSyncing = float64(1)
	executionClientOK      = float64(2)
)

var (
	// TODO: rename "eth1" in metrics
	eventProcessed = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "ssv_eth1_sync_count_success",
		Help: "Count succeeded execution client events",
	}, []string{"etype"})
	eventProcessingFailed = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "ssv_eth1_sync_count_failed",
		Help: "Count failed execution client events",
	}, []string{"etype"})
	executionClientStatus = promauto.NewGauge(prometheus.GaugeOpts{
		Name: "ssv_eth1_status",
		Help: "Status of the connected execution client",
	})
)

type MetricsReporter struct {
	logger *zap.Logger
}

func New(opts ...Option) *MetricsReporter {
	mr := &MetricsReporter{
		logger: zap.NewNop(),
	}

	for _, opt := range opts {
		opt(mr)
	}

	allMetrics := []prometheus.Collector{
		eventProcessed,
		eventProcessingFailed,
		executionClientStatus,
	}

	for i, c := range allMetrics {
		if err := prometheus.Register(c); err != nil {
			// TODO: think how to print metric name
			mr.logger.Warn("could not register prometheus collector", zap.Int("index", i))
		}
	}

	return &MetricsReporter{}
}

func (m MetricsReporter) ExecutionClientReady() {
	executionClientStatus.Set(executionClientOK)
}

func (m MetricsReporter) ExecutionClientSyncing() {
	executionClientStatus.Set(executionClientSyncing)
}

func (m MetricsReporter) ExecutionClientFailure() {
	executionClientStatus.Set(executionClientFailure)
}

func (m MetricsReporter) LastFetchedBlock(block uint64) {

}

func (m MetricsReporter) OperatorHasPublicKey(operatorID spectypes.OperatorID, publicKey []byte) {

}

func (m MetricsReporter) ValidatorInactive(publicKey []byte) {

}

func (m MetricsReporter) ValidatorError(publicKey []byte) {

}

func (m MetricsReporter) ValidatorRemoved(publicKey []byte) {

}

func (m MetricsReporter) EventProcessed(eventName string) {
	eventProcessed.WithLabelValues(eventName).Inc()
}

func (m MetricsReporter) EventProcessingFailed(eventName string) {
	eventProcessingFailed.WithLabelValues(eventName).Inc()
}
