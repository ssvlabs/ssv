package qbft

import (
	"github.com/bloxapp/ssv/protocol/v1/message"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"log"
	"strconv"
)

var (
	metricsDecidedSigners = promauto.NewGaugeVec(prometheus.GaugeOpts{
		Name: "ssv:ibft:last_decided_signers",
		Help: "The highest decided sequence number",
	}, []string{"lambda", "pubKey", "nodeId"})
)

func init() {
	if err := prometheus.Register(metricsDecidedSigners); err != nil {
		log.Println("could not register prometheus collector")
	}
}

// ReportDecided reports on a decided message
func ReportDecided(pk string, msg *message.SignedMessage) {
	for _, nodeID := range msg.Signers {
		metricsDecidedSigners.WithLabelValues(msg.Message.Identifier.GetRoleType().String(), pk, strconv.FormatUint(uint64(nodeID), 10)).Set(float64(msg.Message.Height))
	}
}
