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
// TODO: remove logs, break into smaller validators?
func NewSSVMsgValidator(plogger *zap.Logger, fork forks.Fork, self peer.ID) func(ctx context.Context, p peer.ID, msg *pubsub.Message) pubsub.ValidationResult {
	return func(ctx context.Context, p peer.ID, pmsg *pubsub.Message) pubsub.ValidationResult {
		logger := plogger.With(zap.String("topic", pmsg.GetTopic()), zap.String("peer", p.String()))
		//logger.Debug("validating msg")
		if len(pmsg.GetData()) == 0 {
			logger.Debug("invalid: no data")
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
			logger.Debug("invalid: can't decode message", zap.Error(err))
			reportValidationResult(validationResultEncoding)
			return pubsub.ValidationReject
		}
		// check decided topic
		if msg.MsgType == message.SSVDecidedMsgType {
			if decidedTopic := fork.DecidedTopic(); len(decidedTopic) > 0 {
				if fork.GetTopicFullName(decidedTopic) == pmsg.GetTopic() {
					reportValidationResult(validationResultValid)
					return pubsub.ValidationAccept
				}
			}
		}
		topics := fork.ValidatorTopicID(msg.GetIdentifier().GetValidatorPK())
		// wrong topic
		if fork.GetTopicFullName(topics[0]) != pmsg.GetTopic() {
			// check second topic
			// TODO: remove after forks
			if len(topics) == 1 || fork.GetTopicFullName(topics[1]) != pmsg.GetTopic() {
				logger.Debug("invalid: wrong topic",
					zap.Strings("actual", topics),
					zap.String("expected", fork.GetTopicBaseName(pmsg.GetTopic())),
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
