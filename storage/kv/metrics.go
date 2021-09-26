package kv

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"log"
)

var (
	metricsBadgerLSMSize = promauto.NewGauge(prometheus.GaugeOpts{
		Name: "ssv:badgerdb:size_lsm",
		Help: "The size of Badger LSM",
	})
	metricsBadgerVLOGSize = promauto.NewGauge(prometheus.GaugeOpts{
		Name: "ssv:badgerdb:size_vlog",
		Help: "The size of Badger Value Log",
	})
)

func init() {
	if err := prometheus.Register(metricsBadgerLSMSize); err != nil {
		log.Println("could not register prometheus collector")
	}
	if err := prometheus.Register(metricsBadgerVLOGSize); err != nil {
		log.Println("could not register prometheus collector")
	}
}
