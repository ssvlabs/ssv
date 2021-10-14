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
	metricsRunningIBFTs.WithLabelValues(pubKey).Set(float64(ibftRunning))

	metricsCurrentSlot.WithLabelValues(pubKey).Set(float64(duty.Slot))

	return func() {
		metricsRunningIBFTsCount.Dec()
		metricsRunningIBFTs.WithLabelValues(pubKey).Set(float64(ibftIdle))
	}
}

// ReportIBFTStatus reports the current iBFT status
func ReportIBFTStatus(pk string, finished, errorFound bool) {
	if errorFound {
		metricsRunningIBFTs.WithLabelValues(pk).Set(float64(ibftErrored))
	} else {
		if finished {
			metricsRunningIBFTs.WithLabelValues(pk).Set(float64(ibftInitialized))
		} else {
			metricsRunningIBFTs.WithLabelValues(pk).Set(float64(ibftInitializing))
		}
	}
}

// ReportValidatorStatus reports the current status of validator
func ReportValidatorStatus(pk string, meta *beacon.ValidatorMetadata, logger *zap.Logger) {
	logger = logger.With(zap.String("pubKey", pk), zap.String("who", "ReportValidatorStatus"),
		zap.Any("metadata", meta))
	if meta == nil {
		logger.Debug("validator metadata not found")
		metricsValidatorStatus.WithLabelValues(pk).Set(float64(validatorStatusNotFound))
	} else if meta.Slashed() {
		logger.Debug("validator slashed")
		metricsValidatorStatus.WithLabelValues(pk).Set(float64(validatorStatusSlashed))
	} else if meta.Exiting() {
		logger.Debug("validator exiting / exited")
		metricsValidatorStatus.WithLabelValues(pk).Set(float64(validatorStatusExiting))
	} else if !meta.Activated() {
		logger.Debug("validator not activated")
		metricsValidatorStatus.WithLabelValues(pk).Set(float64(validatorStatusNotActivated))
	//} else if meta.Pending() {
	//	logger.Debug("validator pending")
	//	metricsValidatorStatus.WithLabelValues(pk).Set(float64(validatorStatusPending))
	} else if meta.Index == 0 {
		logger.Debug("validator index not found")
		metricsValidatorStatus.WithLabelValues(pk).Set(float64(validatorStatusNoIndex))
	} else {
		logger.Debug("validator is ready")
		metricsValidatorStatus.WithLabelValues(pk).Set(float64(validatorStatusReady))
	}
}

type validatorStatus int32
type ibftStatus int32

var (
	validatorStatusInactive     validatorStatus = 0
	validatorStatusNoIndex      validatorStatus = 1
	validatorStatusError        validatorStatus = 2
	validatorStatusReady        validatorStatus = 3
	validatorStatusNotActivated validatorStatus = 4
	validatorStatusExiting      validatorStatus = 5
	validatorStatusSlashed      validatorStatus = 6
	validatorStatusNotFound     validatorStatus = 7
	//validatorStatusPending      validatorStatus = 8
)

var (
	ibftIdle         ibftStatus = 0
	ibftRunning      ibftStatus = 1
	ibftInitializing ibftStatus = 2
	ibftInitialized  ibftStatus = 3
	ibftErrored      ibftStatus = 4
)
