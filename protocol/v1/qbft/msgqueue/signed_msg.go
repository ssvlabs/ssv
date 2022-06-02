package msgqueue

import (
	"github.com/bloxapp/ssv/protocol/v1/message"
)

// SignedMsgCleaner cleans consensus messages from the queue
// it will clean messages of the given identifier and under the given height
func SignedMsgCleaner(mid message.Identifier, h message.Height) Cleaner {
	identifier := mid.String()
	return func(k Index) bool {
		if k.Mt != message.SSVConsensusMsgType {
			return false
		}
		if k.ID != identifier {
			return false
		}
		if k.H > h {
			return false
		}
		// clean
		return true
	}
}

func signedMsgIndexValidator(msg *message.SSVMessage) *message.SignedMessage {
	if msg == nil {
		return nil
	}
	if msg.MsgType != message.SSVConsensusMsgType && msg.MsgType != message.SSVDecidedMsgType {
		return nil
	}
	sm := &message.SignedMessage{}
	if err := sm.Decode(msg.Data); err != nil {
		return nil
	}
	if sm.Message == nil {
		return nil
	}
	return sm
}

// SignedMsgIndexer is the Indexer used for message.SignedMessage
func SignedMsgIndexer() Indexer {
	return func(msg *message.SSVMessage) Index {
		if sm := signedMsgIndexValidator(msg); sm != nil {
			return SignedMsgIndex(msg.MsgType, msg.ID.String(), sm.Message.Height, sm.Message.MsgType)[0]
		}
		return Index{}
	}
}

// SignedMsgIndex indexes a message.SignedMessage by identifier, msg type and height
func SignedMsgIndex(msgType message.MsgType, mid string, h message.Height, cmt ...message.ConsensusMessageType) []Index {
	var res []Index
	for _, mt := range cmt {
		res = append(res, Index{
			Mt:  msgType,
			ID:  mid,
			H:   h,
			Cmt: mt,
		})
		//res = append(res, fmt.Sprintf("/%s/id/%s/height/%d/qbft_msg_type/%s", msgType.String(), mid, h, Mt.String()))
	}
	return res
}

// DecidedMsgIndexer is the Indexer used for decided message.SignedMessage
func DecidedMsgIndexer() Indexer {
	return func(msg *message.SSVMessage) Index {
		if msg.MsgType != message.SSVDecidedMsgType {
			return Index{}
		}
		if sm := signedMsgIndexValidator(msg); sm != nil {
			return DecidedMsgIndex(msg.ID.String())
		}
		return Index{}
	}
}

// DecidedMsgIndex indexes a decided message.SignedMessage by identifier, msg type
func DecidedMsgIndex(mid string) Index {
	return Index{
		Mt:  message.SSVDecidedMsgType,
		ID:  mid,
		Cmt: message.CommitMsgType,
	}
	//return fmt.Sprintf("/%s/id/%s/qbft_msg_type/%s", message.SSVDecidedMsgType.String(), mid, message.CommitMsgType.String())
}

// getRound returns the round of the message if applicable
func getRound(msg *message.SSVMessage) (message.Round, bool) {
	sm := message.SignedMessage{}
	if err := sm.Decode(msg.Data); err != nil {
		return 0, false
	}
	if sm.Message == nil {
		return 0, false
	}
	return sm.Message.Round, true
}

// getConsensusMsgType returns the message.ConsensusMessageType of the message if applicable
func getConsensusMsgType(msg *message.SSVMessage) (message.ConsensusMessageType, bool) {
	sm := message.SignedMessage{}
	if err := sm.Decode(msg.Data); err != nil {
		return 0, false
	}
	if sm.Message == nil {
		return 0, false
	}
	return sm.Message.MsgType, true
}
