package topics

import (
	"context"
	"github.com/bloxapp/ssv/network/forks"
	"github.com/libp2p/go-libp2p-core/peer"
	pubsub "github.com/libp2p/go-libp2p-pubsub"
	"go.uber.org/zap"
)

// MsgValidator represents the interface expected by libp2p
//
type MsgValidator func(ctx context.Context, p peer.ID, msg *pubsub.Message) pubsub.ValidationResult

// newMsgValidator creates a new msg validator
func newMsgValidator(logger *zap.Logger, fork forks.Fork) MsgValidator {
	return func(ctx context.Context, p peer.ID, msg *pubsub.Message) pubsub.ValidationResult {
		if len(msg.Data) == 0 {
			return pubsub.ValidationReject
		}
		nm, err := fork.DecodeNetworkMsg(msg.Data)
		if err != nil {
			return invalidResult(p, msg)
		}
		// TODO: validate msg
		logger.Debug("xxx validate topic msg", zap.ByteString("data", nm.SignedMessage.Message.Lambda))
		return pubsub.ValidationAccept
	}
}

// invalidResult determines whether we should act upon an invalid message
func invalidResult(p peer.ID, msg *pubsub.Message) pubsub.ValidationResult {
	if msg.ReceivedFrom != p {
		// sending peer does not own the message
		return pubsub.ValidationIgnore
	}
	return pubsub.ValidationReject
}
