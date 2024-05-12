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

	pMsg.ValidatorData = d
	return pubsub.ValidationAccept
}
