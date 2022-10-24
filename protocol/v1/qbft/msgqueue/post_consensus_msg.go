package msgqueue

import (
	spec "github.com/attestantio/go-eth2-client/spec/phase0"
	specssv "github.com/bloxapp/ssv-spec/ssv"
	spectypes "github.com/bloxapp/ssv-spec/types"
)

// SignedPostConsensusMsgCleaner cleans post consensus messages from the queue
// it will clean messages of the given identifier and under the given slot
func SignedPostConsensusMsgCleaner(mid spectypes.MessageID, s spec.Slot) Cleaner {
	return func(k Index) bool {
		if k.Mt != spectypes.SSVPartialSignatureMsgType {
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
	return func(msg *spectypes.SSVMessage) Index {
		if msg == nil {
			return Index{}
		}
		if msg.MsgType != spectypes.SSVPartialSignatureMsgType {
			return Index{}
		}
		sm := specssv.SignedPartialSignatureMessage{}
		if err := sm.Decode(msg.Data); err != nil {
			return Index{}
		}
		return SignedPostConsensusMsgIndex(msg.MsgID.String(), sm.Messages[0].Slot)
	}
}

// SignedPostConsensusMsgIndex indexes a message.SignedPostConsensusMessage by identifier and height
func SignedPostConsensusMsgIndex(mid string, s spec.Slot) Index {
	return Index{
		Name: "post_consensus_index",
		Mt:   spectypes.SSVPartialSignatureMsgType,
		ID:   mid,
		S:    s,
		H:    -1,
		Cmt:  -1, // as unknown
	}
}
