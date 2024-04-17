package validation

import (
	pubsub "github.com/libp2p/go-libp2p-pubsub"
	"go.uber.org/zap"

	"github.com/bloxapp/ssv/network/commons"
	"github.com/bloxapp/ssv/protocol/v2/ssv/queue"
)

func (mv *messageValidator) validateSelf(pmsg *pubsub.Message) pubsub.ValidationResult {
	rawMsgPayload, _, _, err := commons.DecodeSignedSSVMessage(pmsg.Data)
	if err != nil {
		mv.logger.Error("failed to decode signed ssv message", zap.Error(err))
		return pubsub.ValidationReject
	}
	msg, err := commons.DecodeNetworkMsg(rawMsgPayload)
	if err != nil {
		mv.logger.Error("failed to decode network message", zap.Error(err))
		return pubsub.ValidationReject
	}
	// skipping the error check for testing simplifying
	decMsg, _ := queue.DecodeSSVMessage(msg)
	pmsg.ValidatorData = decMsg
	return pubsub.ValidationAccept
}
