package msgvalidation

import (
	pubsub "github.com/libp2p/go-libp2p-pubsub"
)

func (mv *messageValidator) validatePubSubMessage(pMsg *pubsub.Message) error {
	if len(pMsg.GetData()) == 0 {
		return ErrPubSubMessageHasNoData
	}

	// Max possible MsgType + MsgID + Data plus 10% for encoding overhead
	// TODO: adjust the number
	const maxMsgSize = 4 + 56 + 8388668
	const maxEncodedMsgSize = maxMsgSize + maxMsgSize/10
	if len(pMsg.GetData()) > maxEncodedMsgSize {
		e := ErrPubSubDataTooBig
		e.got = len(pMsg.GetData())
		return e
	}
	return nil
}
