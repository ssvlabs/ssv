package validation

import (
	pubsub "github.com/libp2p/go-libp2p-pubsub"
)

func (mv *messageValidator) validatePubSubMessage(pMsg *pubsub.Message) error {
	if len(pMsg.GetData()) == 0 {
		return ErrPubSubMessageHasNoData
	}

	if len(pMsg.GetData()) > maxEncodedMsgSize {
		e := ErrPubSubDataTooBig
		e.got = len(pMsg.GetData())
		return e
	}
	return nil
}
