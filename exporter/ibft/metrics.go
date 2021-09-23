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
	metricsDecidedSignersExp = promauto.NewGaugeVec(prometheus.GaugeOpts{
		Name: "ssv:validator:ibft_decided_signers_exp",
		Help: "Signers of the highest decided sequence number",
	}, []string{"lambda", "pubKey", "seq", "nodeId"})
)

func init() {
	if err := prometheus.Register(metricsDecidedSignersExp); err != nil {
		log.Println("could not register prometheus collector")
	}
}

func reportDecided(msg *proto.SignedMessage, share *storage.Share) {
	for _, nodeID := range msg.SignerIds {
		metricsDecidedSignersExp.WithLabelValues(string(msg.Message.GetLambda()),
			share.PublicKey.SerializeToHexStr(), strconv.FormatUint(msg.Message.SeqNumber, 10),
			strconv.FormatUint(nodeID, 10))
	}
}
