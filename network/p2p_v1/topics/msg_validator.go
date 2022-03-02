package topics

import (
	"context"
	"encoding/hex"
	"github.com/bloxapp/ssv/network/forks"
	"github.com/bloxapp/ssv/protocol"
	"github.com/libp2p/go-libp2p-core/peer"
	pubsub "github.com/libp2p/go-libp2p-pubsub"
	"go.uber.org/zap"
)

// newMsgValidator creates a new msg validator
func newMsgValidator(logger *zap.Logger, fork forks.Fork) func(ctx context.Context, p peer.ID, msg *pubsub.Message) pubsub.ValidationResult {
	return func(ctx context.Context, p peer.ID, msg *pubsub.Message) pubsub.ValidationResult {
		logger.Debug("xxx validating topic msg")
		if len(msg.Data) == 0 {
			logger.Warn("xxx no data")
			return pubsub.ValidationReject
		}
		smsg := &protocol.SSVMessage{}
		if err := smsg.Decode(msg.Data); err != nil {
			// can't decode message
			logger.Warn("xxx can't decode message", zap.Error(err))
			return pubsub.ValidationReject
		}
		validatorPKHex := smsg.MsgID.GetValidatorPK()
		vpk, err := hex.DecodeString(string(validatorPKHex))
		if err != nil {
			// can't decode message
			logger.Warn("xxx invalid validator public key", zap.Error(err))
			return pubsub.ValidationReject
		}
		topic := fork.ValidatorTopicID(vpk)
		if topic != *msg.Topic {
			// wrong topic
			logger.Warn("xxx wrong topic", zap.String("actual", topic),
				zap.String("expected", *msg.Topic),
				zap.ByteString("smsg.MsgID", smsg.MsgID))
			return pubsub.ValidationReject
		}
		logger.Debug("xxx validated topic msg", zap.String("topic", topic),
			zap.ByteString("smsg.MsgID", smsg.MsgID))
		return pubsub.ValidationAccept
	}
}
