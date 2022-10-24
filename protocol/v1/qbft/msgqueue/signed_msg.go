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
		if k.Mt != spectypes.SSVConsensusMsgType && k.Mt != spectypes.SSVDecidedMsgType {
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
	if msg.MsgType != spectypes.SSVConsensusMsgType && msg.MsgType != spectypes.SSVDecidedMsgType {
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
			return SignedMsgIndex(msg.MsgType, msg.MsgID.String(), sm.Message.Height, sm.Message.MsgType)[0]
		}
		return Index{}
	}
}

// SignedMsgIndex indexes a specqbft.SignedMessage by identifier, msg type and height
func SignedMsgIndex(msgType spectypes.MsgType, mid string, h specqbft.Height, cmt ...specqbft.MessageType) []Index {
	var res []Index
	for _, mt := range cmt {
		res = append(res, Index{
			Name: "signed_index",
			Mt:   msgType,
			ID:   mid,
			H:    h,
			Cmt:  mt,
		})
		//res = append(res, fmt.Sprintf("/%s/id/%s/height/%d/qbft_msg_type/%s", msgType.String(), mid, h, Mt.String()))
	}
	return res
}

// DecidedMsgIndexer is the Indexer used for decided specqbft.SignedMessage
func DecidedMsgIndexer() Indexer {
	return func(msg *spectypes.SSVMessage) Index {
		if msg.MsgType != spectypes.SSVDecidedMsgType {
			return Index{}
		}
		if sm := signedMsgIndexValidator(msg); sm != nil {
			return DecidedMsgIndex(msg.MsgID.String())
		}
		return Index{}
	}
}

// DecidedMsgIndex indexes a decided specqbft.SignedMessage by identifier, msg type
func DecidedMsgIndex(mid string) Index {
	return Index{
		Name: "decided_index",
		Mt:   spectypes.SSVDecidedMsgType,
		ID:   mid,
		Cmt:  specqbft.CommitMsgType,
		H:    -1,
	}
	//return fmt.Sprintf("/%s/id/%s/qbft_msg_type/%s", message.SSVDecidedMsgType.String(), mid, message.CommitMsgType.String())
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
