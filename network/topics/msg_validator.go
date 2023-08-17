package topics

import (
	"context"
	"errors"
	"time"

	pubsub "github.com/libp2p/go-libp2p-pubsub"
	"github.com/libp2p/go-libp2p/core/peer"
	"go.uber.org/zap"

	"github.com/bloxapp/ssv/message/validation"
	"github.com/bloxapp/ssv/network/forks"
	"github.com/bloxapp/ssv/protocol/v2/ssv/queue"
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

		messageData := pmsg.GetData()
		if len(messageData) == 0 {
			reportValidationResult(validationResultNoData)
			return pubsub.ValidationReject
		}

		// Max possible MsgType + MsgID + Data plus 10% for encoding overhead
		// TODO: check if we need to add 10% here
		const maxMsgSize = 4 + 56 + 8388668
		const maxEncodedMsgSize = maxMsgSize + maxMsgSize/10
		if len(messageData) > maxEncodedMsgSize {
			reportValidationResult(validationResultTooBig)
			return pubsub.ValidationReject
		}

		msg, err := fork.DecodeNetworkMsg(messageData)
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
			decodedMessage, err := validator.ValidateMessage(msg, time.Now())
			if err != nil {
				var valErr validation.Error
				if errors.As(err, &valErr) && valErr.Reject() {
					logger.Debug("rejecting invalid message", zap.Error(err))
					// TODO: consider having metrics for each type of validation error
					// TODO: pass metrics to NewSSVMsgValidator
					reportValidationResult(validationResultInvalidRejected)
					return pubsub.ValidationReject
				}

				logger.Debug("ignoring invalid message", zap.Error(err))
				// TODO: pass metrics to NewSSVMsgValidator
				reportValidationResult(validationResultInvalidIgnored)
				return pubsub.ValidationIgnore
			}

			pmsg.ValidatorData = decodedMessage
		} else {
			decodedMessage, err := queue.DecodeSSVMessage(msg)
			if err != nil {
				logger.Debug("ignoring invalid message", zap.Error(err))

				reportValidationResult(validationResultInvalidIgnored)
				return pubsub.ValidationIgnore
			}

			pmsg.ValidatorData = decodedMessage
		}

		logger.Debug("accepting valid message", zap.Error(err))
		reportValidationResult(validationResultOK)

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
