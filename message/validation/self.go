package validation

import (
	pubsub "github.com/libp2p/go-libp2p-pubsub"
	spectypes "github.com/ssvlabs/ssv-spec/types"
	"go.uber.org/zap"

	"github.com/ssvlabs/ssv/protocol/v2/ssv/queue"
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

	// TODO: (Alan) remove this log when not needed
	// var identity zap.Field
	// if signedSSVMessage.SSVMessage.GetID().GetRoleType() == spectypes.RoleCommittee {
	// 	identity = zap.String("committee_id", hex.EncodeToString(signedSSVMessage.SSVMessage.GetID().GetDutyExecutorID()[16:]))
	// } else {
	// 	identity = zap.String("pubkey", hex.EncodeToString(signedSSVMessage.SSVMessage.GetID().GetDutyExecutorID()))
	// }
	// mv.logger.Debug("got p2p message",
	// 	append([]zap.Field{
	// 		zap.Int("type", int(signedSSVMessage.SSVMessage.MsgType)),
	// 		zap.String("role", signedSSVMessage.SSVMessage.GetID().GetRoleType().String()),
	// 	}, identity)...,
	// )

	pMsg.ValidatorData = d
	return pubsub.ValidationAccept
}
