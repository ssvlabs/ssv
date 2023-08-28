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
// TODO: consider making logging and metrics optional for tests
func NewSSVMsgValidator(logger *zap.Logger, metrics metrics, fork forks.Fork, validator *validation.MessageValidator) func(ctx context.Context, p peer.ID, msg *pubsub.Message) pubsub.ValidationResult {
	return func(ctx context.Context, p peer.ID, pmsg *pubsub.Message) pubsub.ValidationResult {
		start := time.Now()
		var validationDurationLabels []string // TODO: implement

		defer func() {
			metrics.MessageValidationDuration(time.Since(start), validationDurationLabels...)
		}()

		topic := pmsg.GetTopic()

		metricPubsubActiveMsgValidation.WithLabelValues(topic).Inc()
		defer metricPubsubActiveMsgValidation.WithLabelValues(topic).Dec()

		messageData := pmsg.GetData()
		if len(messageData) == 0 {
			metrics.MessageRejected("no data")
			return pubsub.ValidationReject
		}

		metrics.MessageSize(len(messageData))

		// Max possible MsgType + MsgID + Data plus 10% for encoding overhead
		// TODO: check if we need to add 10% here
		const maxMsgSize = 4 + 56 + 8388668
		const maxEncodedMsgSize = maxMsgSize + maxMsgSize/10
		if len(messageData) > maxEncodedMsgSize {
			metrics.MessageRejected("message is too big")
			return pubsub.ValidationReject
		}

		msg, err := fork.DecodeNetworkMsg(messageData)
		if err != nil {
			// can't decode message
			// logger.Debug("invalid: can't decode message", zap.Error(err))
			metrics.MessageRejected("could not decode network message")
			return pubsub.ValidationReject
		}
		if msg == nil {
			metrics.MessageRejected("message is nil")
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

		metrics.SSVMessageType(msg.MsgType)

		if validator != nil {
			// TODO: consider merging NewSSVMsgValidator with validator.ValidateMessage
			decodedMessage, err := validator.ValidateMessage(msg, time.Now())
			if err != nil {
				var valErr validation.Error
				if errors.As(err, &valErr) {
					if valErr.Reject() {
						if !valErr.Silent() {
							logger.Debug("rejecting invalid message", zap.Error(err))
						}
						metrics.MessageRejected(valErr.Text())
						return pubsub.ValidationReject
					} else {
						if !valErr.Silent() {
							logger.Debug("ignoring invalid message", zap.Error(err))
						}
						metrics.MessageIgnored(valErr.Text())
						return pubsub.ValidationIgnore
					}
				} else {
					metrics.MessageIgnored(err.Error())
					logger.Debug("ignoring invalid message", zap.Error(err))
					return pubsub.ValidationIgnore
				}
			}

			pmsg.ValidatorData = decodedMessage
		} else {
			decodedMessage, err := queue.DecodeSSVMessage(msg)
			if err != nil {
				logger.Debug("ignoring invalid message", zap.Error(err))

				metrics.MessageIgnored(err.Error())
				return pubsub.ValidationIgnore
			}

			pmsg.ValidatorData = decodedMessage
		}

		metrics.MessageAccepted()

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
