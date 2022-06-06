package msgqueue

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"log"
)

var (
	metricsMsgQSize = promauto.NewGaugeVec(prometheus.GaugeOpts{
		Name: "ssv:ibft:msgq:size",
		Help: "The amount of messages in queue",
	}, []string{"lambda"})
	metricsMsgQRatio = promauto.NewGaugeVec(prometheus.GaugeOpts{
		Name: "ssv:ibft:msgq:ratio",
		Help: "The messages ratio between pop and add",
	}, []string{"lambda", "index_name", "msg_type", "consensus_type"})
)

func init() {
	if err := prometheus.Register(metricsMsgQSize); err != nil {
		log.Println("could not register prometheus collector")
	}
	if err := prometheus.Register(metricsMsgQRatio); err != nil {
		log.Println("could not register prometheus collector")
	}
}
