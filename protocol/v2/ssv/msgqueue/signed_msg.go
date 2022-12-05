package msgqueue

import (
	specqbft "github.com/bloxapp/ssv-spec/qbft"
	spectypes "github.com/bloxapp/ssv-spec/types"
)

// SignedMsgCleaner cleans consensus messages from the queue
// it will clean messages of the given identifier and under the given height
func SignedMsgCleaner(mid spectypes.MessageID, h specqbft.Height) Cleaner {
	identifier := mid.String()
	return func(k Index) bool {
		if k.Mt != spectypes.SSVConsensusMsgType && !k.D {
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

func signedMsgIndexValidator(msg *spectypes.SSVMessage) *specqbft.SignedMessage {
	if msg == nil {
		return nil
	}
	if msg.MsgType != spectypes.SSVConsensusMsgType {
		return nil
	}
	sm := &specqbft.SignedMessage{}
	if err := sm.Decode(msg.Data); err != nil {
		return nil
	}
	if sm.Message == nil {
		return nil
	}
	return sm
}

// SignedMsgIndexer is the Indexer used for specqbft.SignedMessage
func SignedMsgIndexer() Indexer {
	return func(msg *spectypes.SSVMessage) Index {
		if sm := signedMsgIndexValidator(msg); sm != nil {
			return SignedMsgIndex(msg.MsgType, msg.MsgID.String(), sm.Message.Height, len(sm.GetSigners()) >= 3, sm.Message.MsgType)[0]
		}
		return Index{}
	}
}

// SignedMsgIndex indexes a specqbft.SignedMessage by identifier, msg type, height and decided.
func SignedMsgIndex(msgType spectypes.MsgType, mid string, h specqbft.Height, d bool, cmt ...specqbft.MessageType) []Index {
	var res []Index
	for _, mt := range cmt {
		res = append(res, Index{
			Name: "signed_index",
			Mt:   msgType,
			ID:   mid,
			H:    h,
			Cmt:  mt,
			D:    d,
		})
		// res = append(res, fmt.Sprintf("/%s/id/%s/height/%d/qbft_msg_type/%s", msgType.String(), mid, h, Mt.String()))
	}
	return res
}

// DecidedMsgIndexer is the Indexer used for decided specqbft.SignedMessage
// TODO: identify decided messages by: 1) type commit; 2) quorum of signers
func DecidedMsgIndexer() Indexer {
	return func(msg *spectypes.SSVMessage) Index {
		if msg.MsgType != spectypes.SSVConsensusMsgType {
			return Index{}
		}
		sm := signedMsgIndexValidator(msg)
		if sm == nil {
			return Index{}
		}
		// TODO: use a function to check if decided
		if sm.Message.MsgType != specqbft.CommitMsgType || len(sm.GetSigners()) < 3 {
			return Index{}
		}
		return DecidedMsgIndex(msg.MsgID.String())
	}
}

// DecidedMsgIndex indexes a decided specqbft.SignedMessage by identifier, msg type
func DecidedMsgIndex(mid string) Index {
	return Index{
		Name: "decided_index",
		Mt:   spectypes.SSVConsensusMsgType,
		ID:   mid,
		Cmt:  specqbft.CommitMsgType,
		H:    specqbft.FirstHeight,
		D:    true,
	}
	// return fmt.Sprintf("/%s/id/%s/qbft_msg_type/%s", message.SSVConsensusMsgType.String(), mid, message.CommitMsgType.String())
}

// getRound returns the round of the message if applicable
func getRound(msg *spectypes.SSVMessage) (specqbft.Round, bool) {
	sm := specqbft.SignedMessage{}
	if err := sm.Decode(msg.Data); err != nil {
		return 0, false
	}
	if sm.Message == nil {
		return 0, false
	}
	return sm.Message.Round, true
}

// getConsensusMsgType returns the message.ConsensusMessageType of the message if applicable
func getConsensusMsgType(msg *spectypes.SSVMessage) (specqbft.MessageType, bool) {
	sm := specqbft.SignedMessage{}
	if err := sm.Decode(msg.Data); err != nil {
		return 0, false
	}
	if sm.Message == nil {
		return 0, false
	}
	return sm.Message.MsgType, true
}
