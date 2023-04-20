package validation

import (
	"log"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

var (
	metricPubsubActiveMsgValidation = promauto.NewGaugeVec(prometheus.GaugeOpts{
		Name: "ssv:p2p:pubsub:msg:val:active",
		Help: "Count active message validation",
	}, []string{"topic"})
)

func init() {
	if err := prometheus.Register(metricPubsubActiveMsgValidation); err != nil {
		log.Println("could not register prometheus collector")
	}
}
