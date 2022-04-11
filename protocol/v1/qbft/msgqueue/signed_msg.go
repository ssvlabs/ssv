package msgqueue

import (
	"fmt"
	"github.com/bloxapp/ssv/protocol/v1/message"
	"strconv"
	"strings"
)

// SignedMsgCleaner cleans consensus messages from the queue
// it will clean messages of the given identifier and under the given height
func SignedMsgCleaner(mid message.Identifier, h message.Height) Cleaner {
	return func(k string) bool {
		parts := strings.Split(k, "/")
		if len(parts) < 2 {
			return false // unknown
		}
		parts = parts[1:]
		if parts[0] != message.SSVConsensusMsgType.String() {
			return false
		}
		if parts[2] != fmt.Sprintf("%x", mid) {
			return false
		}
		if getIndexHeight(parts...) > h {
			return false
		}
		// clean
		return true
	}
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
		return SignedMsgIndex(msg.ID, sm.Message.Height, sm.Message.MsgType)
	}
}

// SignedMsgIndex indexes a message.SignedMessage by identifier, msg type and height
func SignedMsgIndex(mid message.Identifier, h message.Height, cmt message.ConsensusMessageType) string {
	return fmt.Sprintf("/%s/id/%x/height/%d/qbft_msg_type/%s", message.SSVConsensusMsgType.String(),
		mid, h, cmt.String())
}

func getIndexHeight(idxParts ...string) message.Height {
	hraw := idxParts[4]
	h, err := strconv.Atoi(hraw)
	if err != nil {
		return 0
	}
	return message.Height(h)
}
