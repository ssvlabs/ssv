package validator

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

var (
	metricMessageDropped = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "ssv:ibft:msgq:drops",
		Help: "The amount of message dropped from the validator's msg queue",
	}, []string{"msg_id"})
)

func init() {
	_ = prometheus.Register(metricMessageDropped)
}
