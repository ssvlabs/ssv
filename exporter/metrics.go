package exporter

import (
	"github.com/bloxapp/ssv/beacon"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"log"
)

var (
	metricsValidatorStatusExp = promauto.NewGaugeVec(prometheus.GaugeOpts{
		Name: "ssv:validator:status_exp",
		Help: "Signers of the highest decided sequence number",
	}, []string{"pubKey"})
)

func init() {
	if err := prometheus.Register(metricsValidatorStatusExp); err != nil {
		log.Println("could not register prometheus collector")
	}
}

// TODO: resolve code duplicate with `validator` package
type validatorStatus int32

var (
	//validatorStatusInactive     validatorStatus = 0
	validatorStatusNoIndex      validatorStatus = 1
	//validatorStatusError        validatorStatus = 2
	validatorStatusReady        validatorStatus = 3
	validatorStatusNotDeposited validatorStatus = 4
	validatorStatusExiting      validatorStatus = 5
	validatorStatusSlashed      validatorStatus = 6
)

func reportValidatorStatus(pk string, meta *beacon.ValidatorMetadata) {
	if meta == nil {
		metricsValidatorStatusExp.WithLabelValues(pk).Set(float64(validatorStatusNoIndex))
	} else if !meta.Deposited() {
		metricsValidatorStatusExp.WithLabelValues(pk).Set(float64(validatorStatusNotDeposited))
	} else if meta.Exiting() {
		metricsValidatorStatusExp.WithLabelValues(pk).Set(float64(validatorStatusExiting))
	} else if meta.Slashed() {
		metricsValidatorStatusExp.WithLabelValues(pk).Set(float64(validatorStatusSlashed))
	} else if meta.Index == 0 {
		metricsValidatorStatusExp.WithLabelValues(pk).Set(float64(validatorStatusNoIndex))
	} else {
		metricsValidatorStatusExp.WithLabelValues(pk).Set(float64(validatorStatusReady))
	}
}
