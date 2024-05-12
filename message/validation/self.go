package validation

import (
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

	// TODO: remove
	// mv.logger.Debug("got p2p message",
	// 	zap.Int("type", int(signedSSVMessage.SSVMessage.MsgType)),
	// 	zap.String("role", signedSSVMessage.SSVMessage.GetID().GetRoleType().String()),
	// 	zap.String("id", hex.EncodeToString(signedSSVMessage.SSVMessage.GetID().GetSenderID()[16:])),
	// )

	pMsg.ValidatorData = d
	return pubsub.ValidationAccept
}
