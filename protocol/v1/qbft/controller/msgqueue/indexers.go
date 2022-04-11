package msgqueue

import (
	"fmt"
	"github.com/bloxapp/ssv/protocol/v1/message"
)

// DefaultMsgIndexer returns the default msg indexer to use for message.SSVMessage
func DefaultMsgIndexer() Indexer {
	return func(msg *message.SSVMessage) string {
		if msg == nil {
			return ""
		}
		return DefaultMsgIndex(msg.MsgType, msg.ID)
	}
}

// DefaultMsgIndex is the default msg index
func DefaultMsgIndex(mt message.MsgType, mid message.Identifier) string {
	return fmt.Sprintf("/%s/id/%x", mt.String(), mid)
}

// SignedMsgIndexer is the Indexer used for message.SignedMessage
func SignedMsgIndexer() Indexer {
	return func(msg *message.SSVMessage) string {
		if msg == nil {
			return ""
		}
		if msg == nil {
			return ""
		}
		sm := message.SignedMessage{}
		if err := sm.Decode(msg.Data); err != nil {
			return ""
		}
		if sm.Message == nil {
			return ""
		}
		return SignedMsgIndex(sm.Message.MsgType, msg.ID, sm.Message.Height)
	}
}

// SignedMsgIndex indexes a message.SignedMessage by identifier, msg type and height
func SignedMsgIndex(cmt message.ConsensusMessageType, mid message.Identifier, h message.Height) string {
	return fmt.Sprintf("/%s/id/%x/msg_type/%s/height/%d", message.SSVConsensusMsgType.String(),
		mid, cmt.String(), h)
}

// SignedPostConsensusMsgIndexer is the Indexer used for message.SignedPostConsensusMessage
func SignedPostConsensusMsgIndexer() Indexer {
	return func(msg *message.SSVMessage) string {
		if msg == nil {
			return ""
		}
		sm := message.SignedPostConsensusMessage{}
		if err := sm.Decode(msg.Data); err != nil {
			return ""
		}
		if sm.Message == nil {
			return ""
		}
		return SignedPostConsensusMsgIndex(msg.ID, sm.Message.Height)
	}
}

// SignedPostConsensusMsgIndex indexes a message.SignedPostConsensusMessage by identifier and height
func SignedPostConsensusMsgIndex(mid message.Identifier, h message.Height) string {
	return fmt.Sprintf("/%s/id/%x/height/%d", message.SSVPostConsensusMsgType.String(), mid, h)
}
