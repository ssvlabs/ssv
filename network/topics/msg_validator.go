package topics

import (
	"context"

	pubsub "github.com/libp2p/go-libp2p-pubsub"
	"github.com/libp2p/go-libp2p/core/peer"
	"go.uber.org/zap"

	"github.com/bloxapp/ssv/message/validation"
	"github.com/bloxapp/ssv/network/forks"
)

// MsgValidatorFunc represents a message validator
type MsgValidatorFunc = func(ctx context.Context, p peer.ID, msg *pubsub.Message) pubsub.ValidationResult

// NewSSVMsgValidator creates a new msg validator that validates message structure,
// and checks that the message was sent on the right topic.
// TODO: enable post SSZ change, remove logs, break into smaller validators?
func NewSSVMsgValidator(logger *zap.Logger, fork forks.Fork, validator *validation.MessageValidator) func(ctx context.Context, p peer.ID, msg *pubsub.Message) pubsub.ValidationResult {
	return func(ctx context.Context, p peer.ID, pmsg *pubsub.Message) pubsub.ValidationResult {
		topic := pmsg.GetTopic()
		metricPubsubActiveMsgValidation.WithLabelValues(topic).Inc()
		defer metricPubsubActiveMsgValidation.WithLabelValues(topic).Dec()
		if len(pmsg.GetData()) == 0 {
			reportValidationResult(validationResultNoData)
			return pubsub.ValidationReject
		}
		msg, err := fork.DecodeNetworkMsg(pmsg.GetData())
		if err != nil {
			// can't decode message
			// logger.Debug("invalid: can't decode message", zap.Error(err))
			reportValidationResult(validationResultEncoding)
			return pubsub.ValidationReject
		}
		if msg == nil {
			reportValidationResult(validationResultEncoding)
			return pubsub.ValidationReject
		}

		// Check if the message was sent on the right topic.
		// currentTopic := pmsg.GetTopic()
		// currentTopicBaseName := fork.GetTopicBaseName(currentTopic)
		// topics := fork.ValidatorTopicID(msg.GetID().GetPubKey())
		// for _, tp := range topics {
		//	if tp == currentTopicBaseName {
		//		reportValidationResult(validationResultValid)
		//		return pubsub.ValidationAccept
		//	}
		//}
		// reportValidationResult(validationResultTopic)
		// return pubsub.ValidationReject

		if validator != nil {
			decodedMessage, err := validator.ValidateMessage(msg)
			if err != nil {
				logger.Debug("rejecting invalid message", zap.Error(err))
				// TODO: pass metrics to NewSSVMsgValidator
				reportValidationResult(validationResultInvalid)
				return pubsub.ValidationReject
			}

			pmsg.ValidatorData = decodedMessage
		}

		return pubsub.ValidationAccept
	}
}

//// CombineMsgValidators executes multiple validators
// func CombineMsgValidators(validators ...MsgValidatorFunc) MsgValidatorFunc {
//	return func(ctx context.Context, p peer.ID, msg *pubsub.Message) pubsub.ValidationResult {
//		res := pubsub.ValidationAccept
//		for _, v := range validators {
//			if res = v(ctx, p, msg); res == pubsub.ValidationReject {
//				break
//			}
//		}
//		return res
//	}
//}
