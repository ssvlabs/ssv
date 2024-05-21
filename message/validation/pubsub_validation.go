package validation

import (
	pubsub "github.com/libp2p/go-libp2p-pubsub"
)

func (mv *messageValidator) validatePubSubMessage(pMsg *pubsub.Message) error {
	// Rule: Pubsub.Message.Message.Data must not be empty
	if len(pMsg.GetData()) == 0 {
		return ErrPubSubMessageHasNoData
	}

	// Rule: Pubsub.Message.Message.Data size upper limit
	if len(pMsg.GetData()) > maxEncodedMsgSize {
		e := ErrPubSubDataTooBig
		e.got = len(pMsg.GetData())
		return e
	}
	return nil
}
