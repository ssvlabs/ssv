package topics

import (
	"bytes"
	"context"
	"github.com/bloxapp/ssv/network/forks"
	"github.com/bloxapp/ssv/protocol/v1/message"
	"github.com/libp2p/go-libp2p-core/peer"
	pubsub "github.com/libp2p/go-libp2p-pubsub"
	"go.uber.org/zap"
)

// MsgValidatorFunc represents a message validator
type MsgValidatorFunc = func(ctx context.Context, p peer.ID, msg *pubsub.Message) pubsub.ValidationResult

// NewSSVMsgValidator creates a new msg validator that validates message structure,
// and checks that the message was sent on the right topic.
// TODO: enable post SSZ change, remove logs, break into smaller validators?
func NewSSVMsgValidator(plogger *zap.Logger, fork forks.Fork, self peer.ID) func(ctx context.Context, p peer.ID, msg *pubsub.Message) pubsub.ValidationResult {
	return func(ctx context.Context, p peer.ID, pmsg *pubsub.Message) pubsub.ValidationResult {
		topic := pmsg.GetTopic()
		logger := plogger.With(zap.String("topic", topic), zap.String("peer", p.String()))
		metricsPubsubActiveMsgValidation.WithLabelValues(topic).Inc()
		defer metricsPubsubActiveMsgValidation.WithLabelValues(topic).Dec()
		//logger.Debug("validating msg")
		if len(pmsg.GetData()) == 0 {
			//logger.Debug("invalid: no data")
			reportValidationResult(validationResultNoData)
			return pubsub.ValidationReject
		}
		if bytes.Equal([]byte(p), []byte(self)) {
			reportValidationResult(validationResultSelf)
			return pubsub.ValidationAccept
		}
		msg, err := fork.DecodeNetworkMsg(pmsg.GetData())
		if err != nil {
			// can't decode message
			//logger.Debug("invalid: can't decode message", zap.Error(err))
			reportValidationResult(validationResultEncoding)
			return pubsub.ValidationReject
		}
		// check decided topic
		currentTopic := pmsg.GetTopic()
		if msg.MsgType == message.SSVDecidedMsgType {
			if decidedTopic := fork.DecidedTopic(); len(decidedTopic) > 0 {
				if fork.GetTopicFullName(decidedTopic) == currentTopic {
					reportValidationResult(validationResultValid)
					return pubsub.ValidationAccept
				}
			}
		}
		topics := fork.ValidatorTopicID(msg.GetIdentifier().GetValidatorPK())
		// check wrong topic
		if fork.GetTopicFullName(topics[0]) != currentTopic {
			// check second topic
			// TODO: remove after forks
			if len(topics) == 1 || fork.GetTopicFullName(topics[1]) != currentTopic {
				logger.Debug("invalid: wrong topic",
					zap.Strings("actual", topics),
					zap.String("type", msg.MsgType.String()),
					zap.String("expected", fork.GetTopicBaseName(currentTopic)),
					zap.ByteString("smsg.ID", msg.GetIdentifier()))
				reportValidationResult(validationResultTopic)
				return pubsub.ValidationReject
			}
		}
		reportValidationResult(validationResultValid)
		return pubsub.ValidationAccept
	}
}

//// CombineMsgValidators executes multiple validators
//func CombineMsgValidators(validators ...MsgValidatorFunc) MsgValidatorFunc {
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
