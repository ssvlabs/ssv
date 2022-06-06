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
	metricsMsgQPop = promauto.NewGaugeVec(prometheus.GaugeOpts{
		Name: "ssv:ibft:msgq:pop",
		Help: "The amount of messages that were pop-ed from the queue",
	}, []string{"lambda", "msg_type"})
)

func init() {
	if err := prometheus.Register(metricsMsgQSize); err != nil {
		log.Println("could not register prometheus collector")
	}
	if err := prometheus.Register(metricsMsgQPop); err != nil {
		log.Println("could not register prometheus collector")
	}
}
