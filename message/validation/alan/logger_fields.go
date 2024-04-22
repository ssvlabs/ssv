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

// ConsensusFields provides details about the consensus for a message. It's used for logging and metrics.
type ConsensusFields struct {
	Round           specqbft.Round
	QBFTMessageType specqbft.MessageType
	Signers         []spectypes.OperatorID
	Committee       []*spectypes.Operator
}

// LoggerFields provides details about a message. It's used for logging and metrics.
type LoggerFields struct {
	SenderID       []byte
	Role           spectypes.RunnerRole
	SSVMessageType spectypes.MsgType
	Slot           phase0.Slot
	Consensus      *ConsensusFields
}

// Fields returns zap logging fields for the descriptor.
func (d LoggerFields) Fields() []zapcore.Field {
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

func (mv *messageValidator) buildLoggerFields(decodedMessage *DecodedMessage) *LoggerFields {
	descriptor := &LoggerFields{
		Consensus: &ConsensusFields{},
	}

	if decodedMessage == nil {
		return descriptor
	}

	descriptor.SenderID = decodedMessage.SignedSSVMessage.SSVMessage.GetID().GetSenderID()
	descriptor.Role = decodedMessage.SignedSSVMessage.SSVMessage.GetID().GetRoleType()
	descriptor.SSVMessageType = decodedMessage.SignedSSVMessage.SSVMessage.MsgType
	descriptor.Consensus.Signers = decodedMessage.SignedSSVMessage.GetOperatorIDs()

	switch m := decodedMessage.Body.(type) {
	case *specqbft.Message:
		descriptor.Slot = phase0.Slot(m.Height)
		descriptor.Consensus.Round = m.Round
		descriptor.Consensus.QBFTMessageType = m.MsgType
		//descriptor.Consensus.Committee = m // TODO: can be removed?
	case *spectypes.PartialSignatureMessages:
		descriptor.Slot = m.Slot
	}

	return descriptor
}
