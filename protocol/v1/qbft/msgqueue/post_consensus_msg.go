package msgqueue

import (
	"github.com/bloxapp/ssv/protocol/v1/message"
)

// SignedPostConsensusMsgCleaner cleans post consensus messages from the queue
// it will clean messages of the given identifier and under the given height
func SignedPostConsensusMsgCleaner(mid message.Identifier, h message.Height) Cleaner {
	return func(k Index) bool {
		if k.Mt != message.SSVPostConsensusMsgType {
			return false
		}
		if k.ID != mid.String() {
			return false
		}
		if k.H > h {
			return false
		}
		// clean
		return true
	}
}

// SignedPostConsensusMsgIndexer is the Indexer used for message.SignedPostConsensusMessage
func SignedPostConsensusMsgIndexer() Indexer {
	return func(msg *message.SSVMessage) Index {
		if msg == nil {
			return Index{}
		}
		if msg.MsgType != message.SSVPostConsensusMsgType {
			return Index{}
		}
		sm := message.SignedPostConsensusMessage{}
		if err := sm.Decode(msg.Data); err != nil {
			return Index{}
		}
		if sm.Message == nil {
			return Index{}
		}
		return SignedPostConsensusMsgIndex(msg.ID.String(), sm.Message.Height)
	}
}

// SignedPostConsensusMsgIndex indexes a message.SignedPostConsensusMessage by identifier and height
func SignedPostConsensusMsgIndex(mid string, h message.Height) Index {
	return Index{
		Mt:  message.SSVPostConsensusMsgType,
		ID:  mid,
		H:   h,
		Cmt: -1, // as unknown
	}
	//return fmt.Sprintf("/%s/id/%s/height/%d", message.SSVPostConsensusMsgType.String(), mid, h)
}
