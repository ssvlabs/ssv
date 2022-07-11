package msgqueue

import (
	spec "github.com/attestantio/go-eth2-client/spec/phase0"
	"github.com/bloxapp/ssv-spec/ssv"

	"github.com/bloxapp/ssv/protocol/v1/message"
)

// SignedPostConsensusMsgCleaner cleans post consensus messages from the queue
// it will clean messages of the given identifier and under the given slot
func SignedPostConsensusMsgCleaner(mid message.Identifier, s spec.Slot) Cleaner {
	return func(k Index) bool {
		if k.Mt != message.SSVPostConsensusMsgType {
			return false
		}
		if k.ID != mid.String() {
			return false
		}
		if k.S > s {
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
		sm := ssv.SignedPartialSignatureMessage{}
		if err := sm.Decode(msg.Data); err != nil {
			return Index{}
		}
		err := sm.Validate()
		if err != nil {
			return Index{}
		}
		return SignedPostConsensusMsgIndex(msg.ID.String(), sm.Messages[0].Slot)
	}
}

// SignedPostConsensusMsgIndex indexes a message.SignedPostConsensusMessage by identifier and height
func SignedPostConsensusMsgIndex(mid string, s spec.Slot) Index {
	return Index{
		Name: "post_consensus_index",
		Mt:   message.SSVPostConsensusMsgType,
		ID:   mid,
		S:    s,
		Cmt:  -1, // as unknown
	}
}
