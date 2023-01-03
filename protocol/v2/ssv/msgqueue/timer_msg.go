package msgqueue

import (
	specqbft "github.com/bloxapp/ssv-spec/qbft"
	spectypes "github.com/bloxapp/ssv-spec/types"
	"github.com/bloxapp/ssv/protocol/v2/types"
)

// TimerMsgMsgIndexer is the Indexer used for Timer msg type
func TimerMsgMsgIndexer() Indexer {
	return func(msg *spectypes.SSVMessage) Index {
		if msg == nil {
			return Index{}
		}
		if msg.MsgType != types.SSVTimerMsgType {
			return Index{}
		}
		sm := &specqbft.SignedMessage{}
		if err := sm.Decode(msg.Data); err != nil {
			return Index{}
		}
		if sm.Message == nil {
			return Index{}
		}
		return TimerMsgIndex(msg.MsgID.String(), sm.Message.Height)
	}
}

// TimerMsgIndex indexes a timer msg by identifier and height
func TimerMsgIndex(mid string, height specqbft.Height) Index {
	return Index{
		Name: "timer_index",
		Mt:   types.SSVTimerMsgType,
		ID:   mid,
		H:    height,
		Cmt:  -1, // as unknown
		D:    false,
	}
}
