package p2pv1

import (
	"github.com/bloxapp/ssv/network"
	ssv_peers "github.com/bloxapp/ssv/network/p2p/peers"
	"github.com/bloxapp/ssv/protocol/v1/core"
	"go.uber.org/zap"
	"math"
)

// ReportValidation reports the result for the given message
// the result will be converted to a score and reported to peers.ScoreIndex
func (n *p2pNetwork) ReportValidation(msg core.SSVMessage, res network.MsgValidationResult) {
	peers := n.msgResolver.GetPeers(msg.GetData())
	for _, pi := range peers {
		err := n.idx.Score(pi, ssv_peers.NodeScore{Name: "validation", Value: msgValidationScore(res)})
		if err != nil {
			n.logger.Warn("could not score peer", zap.String("peer", pi.String()), zap.Error(err))
			continue
		}
	}
}

const (
	validationScoreLow = 5.0
)

func msgValidationScore(res network.MsgValidationResult) float64 {
	switch res {
	case network.ValidationAccept:
		return validationScoreLow
	case network.ValidationIgnore:
		return 0.0
	case network.ValidationRejectLow:
		return -validationScoreLow
	case network.ValidationRejectMedium:
		return -math.Pow(validationScoreLow, 2.0)
	case network.ValidationRejectHigh:
		return -math.Pow(validationScoreLow, 3.0)
	default:
	}
	return 0
}
