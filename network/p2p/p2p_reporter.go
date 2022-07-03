package p2pv1

import (
	"math"

	"go.uber.org/zap"

	ssvpeers "github.com/bloxapp/ssv/network/peers"
	"github.com/bloxapp/ssv/protocol/v1/message"
	protocolp2p "github.com/bloxapp/ssv/protocol/v1/p2p"
)

// ReportValidation reports the result for the given message
// the result will be converted to a score and reported to peers.ScoreIndex
func (n *p2pNetwork) ReportValidation(msg *message.SSVMessage, res protocolp2p.MsgValidationResult) {
	if !n.isReady() {
		return
	}
	data, err := n.fork.EncodeNetworkMsg(msg)
	if err != nil {
		n.logger.Warn("could not encode message", zap.Error(err))
		return
	}
	peers := n.msgResolver.GetPeers(data)
	for _, pi := range peers {
		err := n.idx.Score(pi, &ssvpeers.NodeScore{Name: "validation", Value: msgValidationScore(res)})
		if err != nil {
			n.logger.Warn("could not score peer", zap.String("peer", pi.String()), zap.Error(err))
			continue
		}
	}
}

const (
	validationScoreLow = 5.0
)

func msgValidationScore(res protocolp2p.MsgValidationResult) float64 {
	switch res {
	case protocolp2p.ValidationAccept:
		return validationScoreLow
	case protocolp2p.ValidationIgnore:
		return 0.0
	case protocolp2p.ValidationRejectLow:
		return -validationScoreLow
	case protocolp2p.ValidationRejectMedium:
		return -math.Pow(validationScoreLow, 2.0)
	case protocolp2p.ValidationRejectHigh:
		return -math.Pow(validationScoreLow, 3.0)
	default:
	}
	return 0
}
