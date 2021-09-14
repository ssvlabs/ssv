package ibft

import (
	"github.com/bloxapp/ssv/ibft/proto"
	"github.com/bloxapp/ssv/validator/storage"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"log"
	"strconv"
)

var (
	metricsHighestDecidedSigners = promauto.NewGaugeVec(prometheus.GaugeOpts{
		Name: "ssv:validator:ibft_decided_signers_exp",
		Help: "The highest decided sequence number",
	}, []string{"lambda", "pubKey", "nodeId"})
)

func init() {
	if err := prometheus.Register(metricsHighestDecidedSigners); err != nil {
		log.Println("could not register prometheus collector")
	}
}

func reportDecided(msg *proto.SignedMessage, share *storage.Share) {
	for _, nodeID := range msg.SignerIds {
		metricsHighestDecidedSigners.WithLabelValues(string(msg.Message.GetLambda()),
			share.PublicKey.SerializeToHexStr(), strconv.FormatUint(nodeID, 10))
	}
}