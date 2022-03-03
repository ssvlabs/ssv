package topics

import (
	"bytes"
	"context"
	"github.com/bloxapp/ssv/network/forks"
	"github.com/bloxapp/ssv/protocol"
	"github.com/libp2p/go-libp2p-core/peer"
	pubsub "github.com/libp2p/go-libp2p-pubsub"
	"go.uber.org/zap"
)

// MsgValidatorFunc represents a message validator
type MsgValidatorFunc = func(ctx context.Context, p peer.ID, msg *pubsub.Message) pubsub.ValidationResult

// newMsgValidator creates a new msg validator
func newMsgValidator(plogger *zap.Logger, fork forks.Fork, self peer.ID) func(ctx context.Context, p peer.ID, msg *pubsub.Message) pubsub.ValidationResult {
	return func(ctx context.Context, p peer.ID, msg *pubsub.Message) pubsub.ValidationResult {
		logger := plogger.With(zap.String("topic", msg.GetTopic()), zap.String("peer", p.String()))
		logger.Debug("xxx validating msg")
		if len(msg.Data) == 0 {
			logger.Warn("xxx no data")
			return pubsub.ValidationReject
		}
		if bytes.Equal([]byte(p), []byte(self)) {
			logger.Warn("xxx our node's message")
			return pubsub.ValidationAccept
		}
		smsg := &protocol.SSVMessage{}
		if err := smsg.Decode(msg.Data); err != nil {
			// can't decode message
			logger.Warn("xxx can't decode message", zap.Error(err))
			return pubsub.ValidationReject
		}
		topic := fork.ValidatorTopicID(smsg.ID.GetValidatorPK())
		if topic != *msg.Topic {
			// wrong topic
			logger.Warn("xxx wrong topic", zap.String("actual", topic),
				zap.String("expected", *msg.Topic),
				zap.ByteString("smsg.ID", smsg.ID))
			return pubsub.ValidationReject
		}
		logger.Debug("xxx validated topic msg", zap.String("topic", topic),
			zap.ByteString("smsg.ID", smsg.ID))
		return pubsub.ValidationAccept
	}
}
