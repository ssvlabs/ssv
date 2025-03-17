package validation

import (
	pubsub "github.com/libp2p/go-libp2p-pubsub"
)

func (mv *messageValidator) validatePubSubMessage(pMsg *pubsub.Message) error {
	// Rule: Pubsub.Message.Message.Data must not be empty
	if len(pMsg.GetData()) == 0 {
		return ErrPubSubMessageHasNoData
	}

	maxMsgSize := MaxEncodedMsgSize

	if mv.netCfg.Beacon.EstimatedCurrentEpoch() >= mv.pectraForkEpoch {
		maxMsgSize = PecraMaxEncodedMsgSize
	}

	// Rule: Pubsub.Message.Message.Data size upper limit
	if len(pMsg.GetData()) > maxMsgSize {
		e := ErrPubSubDataTooBig
		e.got = len(pMsg.GetData())
		return e
	}
	return nil
}
