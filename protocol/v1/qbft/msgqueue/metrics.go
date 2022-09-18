package msgqueue

import (
	"log"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

var (
	metricsMsgQRatio = promauto.NewGaugeVec(prometheus.GaugeOpts{
		Name: "ssv:ibft:msgq:ratio",
		Help: "The messages ratio between pop and add",
	}, []string{"identifier", "index_name", "msg_type", "consensus_type"})
)

func init() {
	if err := prometheus.Register(metricsMsgQRatio); err != nil {
		log.Println("could not register prometheus collector")
	}
}
