package msgqueue

import (
	"fmt"
	"github.com/bloxapp/ssv/protocol/v1/message"
	"strings"
)

// SignedPostConsensusMsgCleaner cleans post consensus messages from the queue
// it will clean messages of the given identifier and under the given height
func SignedPostConsensusMsgCleaner(mid message.Identifier, h message.Height) Cleaner {
	return func(k string) bool {
		parts := strings.Split(k, "/")
		if len(parts) < 2 {
			return false // unknown
		}
		parts = parts[1:] // remove empty string
		if parts[0] != message.SSVPostConsensusMsgType.String() {
			return false
		}
		if parts[2] != mid.String() {
			return false
		}
		if getIndexHeight(parts...) > h {
			return false
		}
		// clean
		return true
	}
}

// SignedPostConsensusMsgIndexer is the Indexer used for message.SignedPostConsensusMessage
func SignedPostConsensusMsgIndexer() Indexer {
	return func(msg *message.SSVMessage) string {
		if msg == nil {
			return ""
		}
		if msg.MsgType != message.SSVPostConsensusMsgType {
			return ""
		}
		sm := message.SignedPostConsensusMessage{}
		if err := sm.Decode(msg.Data); err != nil {
			return ""
		}
		if sm.Message == nil {
			return ""
		}
		return SignedPostConsensusMsgIndex(msg.ID.String(), sm.Message.Height)
	}
}

// SignedPostConsensusMsgIndex indexes a message.SignedPostConsensusMessage by identifier and height
func SignedPostConsensusMsgIndex(mid string, h message.Height) string {
	return fmt.Sprintf("/%s/id/%s/height/%d", message.SSVPostConsensusMsgType.String(), mid, h)
}
