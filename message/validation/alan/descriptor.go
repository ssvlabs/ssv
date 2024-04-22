package validation

import (
	"github.com/attestantio/go-eth2-client/spec/phase0"
	specqbft "github.com/bloxapp/ssv-spec/alan/qbft"
	spectypes "github.com/bloxapp/ssv-spec/alan/types"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"

	"github.com/bloxapp/ssv/logging/fields"
	ssvmessage "github.com/bloxapp/ssv/protocol/v2/message"
)

// ConsensusDescriptor provides details about the consensus for a message. It's used for logging and metrics.
type ConsensusDescriptor struct {
	Round           specqbft.Round
	QBFTMessageType specqbft.MessageType
	Signers         []spectypes.OperatorID
	Committee       []*spectypes.Operator
}

// DebugDescriptor provides details about a message. It's used for logging and metrics.
// TODO: consider using context.Context
type DebugDescriptor struct {
	SenderID       []byte
	Role           spectypes.RunnerRole
	SSVMessageType spectypes.MsgType
	Slot           phase0.Slot
	Consensus      *ConsensusDescriptor
}

// Fields returns zap logging fields for the descriptor.
func (d DebugDescriptor) Fields() []zapcore.Field {
	result := []zapcore.Field{
		fields.SenderID(d.SenderID),
		fields.Role(d.Role),
		zap.String("ssv_message_type", ssvmessage.MsgTypeToString(d.SSVMessageType)),
		fields.Slot(d.Slot),
	}

	if d.Consensus != nil {
		var committee []spectypes.OperatorID
		for _, o := range d.Consensus.Committee {
			committee = append(committee, o.OperatorID)
		}

		result = append(result,
			fields.Round(d.Consensus.Round),
			zap.String("qbft_message_type", ssvmessage.QBFTMsgTypeToString(d.Consensus.QBFTMessageType)),
			zap.Uint64s("signers", d.Consensus.Signers),
			zap.Uint64s("committee", committee),
		)
	}

	return result
}
