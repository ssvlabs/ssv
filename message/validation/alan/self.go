package msgvalidation

import (
	specqbft "github.com/bloxapp/ssv-spec/alan/qbft"
	spectypes "github.com/bloxapp/ssv-spec/alan/types"
	pubsub "github.com/libp2p/go-libp2p-pubsub"
	"go.uber.org/zap"

	"github.com/bloxapp/ssv/logging/fields"
)

func (mv *messageValidator) validateSelf(pMsg *pubsub.Message) pubsub.ValidationResult {
	signedSSVMessage := &spectypes.SignedSSVMessage{}
	if err := signedSSVMessage.Decode(pMsg.GetData()); err != nil {
		mv.logger.Error("failed to decode signed ssv message", zap.Error(err))
		return pubsub.ValidationReject
	}

	decodedMessage := &DecodedMessage{
		SignedSSVMessage: signedSSVMessage,
	}

	switch signedSSVMessage.SSVMessage.MsgType {
	case spectypes.SSVConsensusMsgType:
		consensusMessage, err := specqbft.DecodeMessage(signedSSVMessage.GetSSVMessage().Data)
		if err != nil {
			mv.logger.Error("failed to decode consensus message", zap.Error(err))
			return pubsub.ValidationReject
		}

		decodedMessage.Body = consensusMessage

	case spectypes.SSVPartialSignatureMsgType:
		partialSignatureMessages := &spectypes.PartialSignatureMessages{}
		if err := partialSignatureMessages.Decode(signedSSVMessage.GetSSVMessage().Data); err != nil {
			mv.logger.Error("failed to decode partial signature messages", zap.Error(err))
			return pubsub.ValidationReject
		}

		decodedMessage.Body = partialSignatureMessages

	default:
		mv.logger.Error("unsupported message type", fields.MessageType(signedSSVMessage.SSVMessage.MsgType))
	}

	pMsg.ValidatorData = decodedMessage
	return pubsub.ValidationAccept
}
