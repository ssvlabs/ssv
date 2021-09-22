package validator

import (
	"github.com/bloxapp/ssv/beacon"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"go.uber.org/zap"
	"log"
)

var (
	metricsRunningIBFTsCount = promauto.NewGauge(prometheus.GaugeOpts{
		Name: "ssv:validator:running_ibfts_count_all",
		Help: "Count all running IBFTs",
	})
	metricsRunningIBFTs = promauto.NewGaugeVec(prometheus.GaugeOpts{
		Name: "ssv:validator:running_ibfts_count",
		Help: "Count running IBFTs by validator pub key",
	}, []string{"pubKey"})
	metricsCurrentSlot = promauto.NewGaugeVec(prometheus.GaugeOpts{
		Name: "ssv:validator:ibft_current_slot",
		Help: "Current running slot",
	}, []string{"pubKey"})
	metricsValidatorStatus = promauto.NewGaugeVec(prometheus.GaugeOpts{
		Name: "ssv:validator:status",
		Help: "Validator status",
	}, []string{"pubKey"})
)

func init() {
	if err := prometheus.Register(metricsRunningIBFTsCount); err != nil {
		log.Println("could not register prometheus collector")
	}
	if err := prometheus.Register(metricsRunningIBFTs); err != nil {
		log.Println("could not register prometheus collector")
	}
	if err := prometheus.Register(metricsCurrentSlot); err != nil {
		log.Println("could not register prometheus collector")
	}
	if err := prometheus.Register(metricsValidatorStatus); err != nil {
		log.Println("could not register prometheus collector")
	}
}

// reportDutyExecutionMetrics reports duty execution metrics, returns done function to be called once duty is done
func (v *Validator) reportDutyExecutionMetrics(duty *beacon.Duty) func() {
	// reporting metrics
	metricsRunningIBFTsCount.Inc()

	pubKey := v.Share.PublicKey.SerializeToHexStr()
	metricsRunningIBFTs.WithLabelValues(pubKey).Inc()

	metricsCurrentSlot.WithLabelValues(pubKey).Set(float64(duty.Slot))

	return func() {
		metricsRunningIBFTsCount.Dec()
		metricsRunningIBFTs.WithLabelValues(pubKey).Dec()
	}
}

func reportValidatorStatus(pk string, meta *beacon.ValidatorMetadata, logger *zap.Logger) {
	logger = logger.With(zap.String("pubKey", pk), zap.String("who", "reportValidatorStatus"))
	if meta == nil {
		logger.Warn("validator metadata not found")
		metricsValidatorStatus.WithLabelValues(pk).Set(float64(validatorStatusNoIndex))
	} else if !meta.Deposited() {
		logger.Warn("validator not deposited")
		metricsValidatorStatus.WithLabelValues(pk).Set(float64(validatorStatusNotDeposited))
	} else if meta.Exiting() {
		logger.Warn("validator exiting/ exited")
		metricsValidatorStatus.WithLabelValues(pk).Set(float64(validatorStatusExiting))
	} else if meta.Slashed() {
		logger.Warn("validator slashed")
		metricsValidatorStatus.WithLabelValues(pk).Set(float64(validatorStatusSlashed))
	} else if meta.Index == 0 {
		logger.Warn("validator index not found")
		metricsValidatorStatus.WithLabelValues(pk).Set(float64(validatorStatusNoIndex))
	}
}

type validatorStatus int32

var (
	validatorStatusInactive     validatorStatus = 0
	validatorStatusNoIndex      validatorStatus = 1
	validatorStatusError        validatorStatus = 2
	validatorStatusReady        validatorStatus = 3
	validatorStatusNotDeposited validatorStatus = 4
	validatorStatusExiting      validatorStatus = 5
	validatorStatusSlashed      validatorStatus = 6
)
