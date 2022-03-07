package p2pv1

import (
	"github.com/bloxapp/ssv/network"
	ssv_peers "github.com/bloxapp/ssv/network/p2p_v1/peers"
	"github.com/bloxapp/ssv/protocol"
	"go.uber.org/zap"
)

// ReportValidation reports the result for the given message
// the result will be converted to a score and reported to peers.ScoreIndex
func (n *p2pNetwork) ReportValidation(msg protocol.SSVMessage, res network.MsgValidationResult) {
	peers := n.msgResolver.GetPeers(msg.GetData())
	for _, pi := range peers {
		err := n.idx.Score(pi, ssv_peers.NodeScore{Name: "validation", Value: msgValidationScore(res)})
		if err != nil {
			n.logger.Warn("could not score peer", zap.String("peer", pi.String()), zap.Error(err))
			continue
		}
	}
}

func msgValidationScore(res network.MsgValidationResult) float64 {
	switch res {
	case network.ValidationAccept:
		return 10.0
	case network.ValidationIgnore:
		return 0.0
	case network.ValidationRejectLow:
		return -10.0
	case network.ValidationRejectMedium:
		return -100.0
	case network.ValidationRejectHigh:
		return -1000.0
	default:
	}
	return 0
}
