package queue

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

var (
	metricMsgQRatio = promauto.NewGaugeVec(prometheus.GaugeOpts{
		Name: "ssv:ibft:msgq:size",
		Help: "The amount of message in the validator's msg queue",
	}, []string{"pk"})
)

func init() {
	_ = prometheus.Register(metricMsgQRatio)
}
