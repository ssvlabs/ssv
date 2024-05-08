package validation

import (
	"encoding/hex"

	spectypes "github.com/ssvlabs/ssv-spec/types"

	pubsub "github.com/libp2p/go-libp2p-pubsub"
	"go.uber.org/zap"

	"github.com/bloxapp/ssv/protocol/v2/ssv/queue"
)

func (mv *messageValidator) validateSelf(pMsg *pubsub.Message) pubsub.ValidationResult {
	signedSSVMessage := &spectypes.SignedSSVMessage{}
	if err := signedSSVMessage.Decode(pMsg.GetData()); err != nil {
		mv.logger.Error("failed to decode signed ssv message", zap.Error(err))
		return pubsub.ValidationReject
	}

	d, err := queue.DecodeSignedSSVMessage(signedSSVMessage)
	if err != nil {
		mv.logger.Error("failed to decode signed ssv message", zap.Error(err))
		return pubsub.ValidationReject
	}

	//
	//switch signedSSVMessage.SSVMessage.MsgType {
	//case spectypes.SSVConsensusMsgType:
	//	consensusMessage, err := specqbft.DecodeMessage(signedSSVMessage.SSVMessage.Data)
	//	if err != nil {
	//		mv.logger.Error("failed to decode consensus message", zap.Error(err))
	//		return pubsub.ValidationReject
	//	}
	//
	//	decodedMessage.Body = consensusMessage
	//
	//case spectypes.SSVPartialSignatureMsgType:
	//	partialSignatureMessages := &spectypes.PartialSignatureMessages{}
	//	if err := partialSignatureMessages.Decode(signedSSVMessage.SSVMessage.Data); err != nil {
	//		mv.logger.Error("failed to decode partial signature messages", zap.Error(err))
	//		return pubsub.ValidationReject
	//	}
	//
	//	decodedMessage.Body = partialSignatureMessages
	//
	//default:
	//	mv.logger.Error("unsupported message type", fields.MessageType(signedSSVMessage.SSVMessage.MsgType))
	//}

	zap.L().Debug("gotp2pmessage",
		zap.Int("type", int(signedSSVMessage.SSVMessage.MsgType)),
		zap.String("role", signedSSVMessage.SSVMessage.GetID().GetRoleType().String()),
		zap.String("id", hex.EncodeToString(signedSSVMessage.SSVMessage.GetID().GetSenderID())),
	)
	pMsg.ValidatorData = d
	return pubsub.ValidationAccept
}
