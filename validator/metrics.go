package validator

import (
	"github.com/bloxapp/ssv/beacon"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
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
}

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