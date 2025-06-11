package validation

import (
	"github.com/attestantio/go-eth2-client/spec/phase0"
	specqbft "github.com/ssvlabs/ssv-spec/qbft"
	spectypes "github.com/ssvlabs/ssv-spec/types"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"

	"github.com/ssvlabs/ssv/logging/fields"
	ssvmessage "github.com/ssvlabs/ssv/protocol/v2/message"
	"github.com/ssvlabs/ssv/protocol/v2/ssv/queue"
)

// ConsensusFields provides details about the consensus for a message. It's used for logging and metrics.
type ConsensusFields struct {
	Round           specqbft.Round
	QBFTMessageType specqbft.MessageType
}

// LoggerFields provides details about a message. It's used for logging and metrics.
type LoggerFields struct {
	DutyExecutorID []byte
	Role           spectypes.RunnerRole
	SSVMessageType spectypes.MsgType
	Slot           phase0.Slot
	Consensus      *ConsensusFields
	DutyID         string
}

// AsZapFields returns zap logging fields for the descriptor.
func (d LoggerFields) AsZapFields() []zapcore.Field {
	result := []zapcore.Field{
		fields.DutyExecutorID(d.DutyExecutorID),
		fields.Role(d.Role),
		zap.String("ssv_message_type", ssvmessage.MsgTypeToString(d.SSVMessageType)),
		fields.Slot(d.Slot),
	}

	if d.DutyID != "" {
		result = append(result, fields.DutyID(d.DutyID))
	}

	if d.Consensus != nil {
		result = append(result,
			fields.Round(d.Consensus.Round),
			zap.String("qbft_message_type", ssvmessage.QBFTMsgTypeToString(d.Consensus.QBFTMessageType)),
		)
	}

	return result
}

func (mv *messageValidator) buildLoggerFields(decodedMessage *queue.SSVMessage) *LoggerFields {
	descriptor := &LoggerFields{
		Consensus: &ConsensusFields{},
	}

	if decodedMessage == nil {
		return descriptor
	}

	if decodedMessage.SSVMessage == nil {
		return descriptor
	}

	descriptor.DutyExecutorID = decodedMessage.SSVMessage.GetID().GetDutyExecutorID()
	descriptor.Role = decodedMessage.SSVMessage.GetID().GetRoleType()
	descriptor.SSVMessageType = decodedMessage.GetType()

	switch m := decodedMessage.Body.(type) {
	case *specqbft.Message:
		if m != nil {
			descriptor.Slot = phase0.Slot(m.Height)
			descriptor.Consensus.Round = m.Round
			descriptor.Consensus.QBFTMessageType = m.MsgType
		}
	case *spectypes.PartialSignatureMessages:
		if m != nil {
			descriptor.Slot = m.Slot
		}
	}

	if mv.logger.Level() == zap.DebugLevel {
		mv.addDutyIDField(descriptor)
	}

	return descriptor
}

func (mv *messageValidator) addDutyIDField(lf *LoggerFields) {
	if lf.Role == spectypes.RoleCommittee {
		c, ok := mv.validatorStore.Committee(spectypes.CommitteeID(lf.DutyExecutorID[16:]))
		if ok {
			lf.DutyID = fields.FormatCommitteeDutyID(c.Operators, mv.netCfg.Beacon.EstimatedEpochAtSlot(lf.Slot), lf.Slot)
		}
	} else {
		// get the validator index from the msgid
		v, ok := mv.validatorStore.Validator(lf.DutyExecutorID)
		if ok {
			lf.DutyID = fields.FormatDutyID(mv.netCfg.Beacon.EstimatedEpochAtSlot(lf.Slot), lf.Slot, lf.Role.String(), v.ValidatorIndex)
		}
	}
}
