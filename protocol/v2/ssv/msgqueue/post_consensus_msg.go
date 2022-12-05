package msgqueue

import (
	specqbft "github.com/bloxapp/ssv-spec/qbft"
	specssv "github.com/bloxapp/ssv-spec/ssv"
	spectypes "github.com/bloxapp/ssv-spec/types"
)

// SignedPostConsensusMsgCleaner cleans post consensus messages from the queue
// it will clean messages of the given identifier and under the given slot
func SignedPostConsensusMsgCleaner(mid spectypes.MessageID) Cleaner {
	return func(k Index) bool {
		if k.Mt != spectypes.SSVPartialSignatureMsgType {
			return false
		}
		if k.ID != mid.String() {
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
		return SignedPostConsensusMsgIndex(msg.MsgID.String())
	}
}

// SignedPostConsensusMsgIndex indexes a message.SignedPostConsensusMessage by identifier and height
func SignedPostConsensusMsgIndex(mid string) Index {
	return Index{
		Name: "post_consensus_index",
		Mt:   spectypes.SSVPartialSignatureMsgType,
		ID:   mid,
		H:    specqbft.FirstHeight,
		Cmt:  -1, // as unknown
		D:    false,
	}
}
