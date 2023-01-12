package msgqueue

import (
	specqbft "github.com/bloxapp/ssv-spec/qbft"
	spectypes "github.com/bloxapp/ssv-spec/types"
	"github.com/bloxapp/ssv/protocol/v2/message"
	"github.com/bloxapp/ssv/protocol/v2/types"
)

// EventMsgMsgIndexer is the Indexer used for SSVEventMsgType
func EventMsgMsgIndexer() Indexer {
	return func(msg *spectypes.SSVMessage) Index {
		if msg == nil {
			return Index{}
		}
		if msg.MsgType != message.SSVEventMsgType {
			return Index{}
		}
		em := &types.EventMsg{}
		if err := em.Decode(msg.Data); err != nil {
			return Index{}
		}

		switch em.Type {
		case types.Timeout:
			td, err := em.GetTimeoutData()
			if err != nil {
				return Index{}
			}
			return EventMsgIndex(msg.MsgID.String(), td.Height, em.Type)
		default:
			return EventMsgIndex(msg.MsgID.String(), 0, em.Type)
		}
	}
}

// EventMsgIndex indexes a event msg by identifier and height
func EventMsgIndex(mid string, height specqbft.Height, eventType types.EventType) Index {
	return Index{
		Name: "event_index",
		Mt:   message.SSVEventMsgType,
		ID:   mid,
		H:    height,
		Cmt:  -1, // as unknown
		D:    false,
		Et:   eventType,
	}
}
