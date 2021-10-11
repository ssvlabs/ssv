package controller

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"log"
)

var (
	// metricsCurrentSequence for current instance
	metricsCurrentSequence = promauto.NewGaugeVec(prometheus.GaugeOpts{
		Name: "ssv:validator:ibft_current_sequence",
		Help: "The highest decided sequence number",
	}, []string{"lambda", "pubKey"})
)

func init() {
	if err := prometheus.Register(metricsCurrentSequence); err != nil {
		log.Println("could not register prometheus collector")
	}
}
