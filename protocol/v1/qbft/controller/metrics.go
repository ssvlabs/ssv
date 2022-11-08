package controller

import (
	"log"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

var (
	allMetrics = []prometheus.Collector{
		metricsCurrentSequence,
		metricsRunningIBFTsCount,
		metricsRunningIBFTs,
		metricsDurationAttestation,
	}
	// metricsCurrentSequence for current instance
	metricsCurrentSequence = promauto.NewGaugeVec(prometheus.GaugeOpts{
		Name: "ssv:validator:ibft_current_sequence",
		Help: "The highest decided sequence number",
	}, []string{"identifier", "pubKey"})
	metricsRunningIBFTsCount = promauto.NewGauge(prometheus.GaugeOpts{
		Name: "ssv:validator:running_ibfts_count_all",
		Help: "Count all running IBFTs",
	})
	metricsRunningIBFTs = promauto.NewGaugeVec(prometheus.GaugeOpts{
		Name: "ssv:validator:running_ibfts_count",
		Help: "Count running IBFTs by validator pub key",
	}, []string{"pubKey"})
	metricsDurationAttestation = promauto.NewHistogramVec(prometheus.HistogramOpts{
		Name:    "ssv:validator:duration_attestation",
		Help:    "Attestation duration (seconds)",
		Buckets: []float64{0.01, 0.05, 0.1, 0.2, 0.5, 1, 2, 5, 10, 60},
	}, []string{"stage", "pubKey"})
)

func init() {
	for _, c := range allMetrics {
		if err := prometheus.Register(c); err != nil {
			log.Println("could not register prometheus collector")
		}
	}
}

type ibftStatus int32

var (
	ibftIdle         ibftStatus = 0
	ibftRunning      ibftStatus = 1
	ibftInitializing ibftStatus = 2
	ibftInitialized  ibftStatus = 3
	ibftErrored      ibftStatus = 4
)

// reportIBFTInstanceStart reports instance metrics, returns done function to be called once instance is done
func reportIBFTInstanceStart(pubKey string) func() {
	// reporting metrics
	metricsRunningIBFTsCount.Inc()

	metricsRunningIBFTs.WithLabelValues(pubKey).Set(float64(ibftRunning))

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
