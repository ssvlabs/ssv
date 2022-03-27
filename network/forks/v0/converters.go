package v0

import (
	"encoding/json"
	"github.com/bloxapp/ssv/ibft/proto"
	"github.com/bloxapp/ssv/network"
	"github.com/bloxapp/ssv/protocol/v1/core"
)

// FromNetworkV0 converts an old message to v1
func FromNetworkV0(msgV0 *network.Message) (*core.SSVMessage, error) {
	msg := core.SSVMessage{}

	switch msgV0.Type {
	case network.NetworkMsg_SyncType:
		msg.MsgType = core.SSVSyncMsgType
		if msgV0.SyncMessage != nil {
			data, err := json.Marshal(msgV0.SyncMessage)
			if err != nil {
				return nil, err
			}
			msg.Data = data
			msg.ID = msgV0.SyncMessage.Lambda
		}
		return &msg, nil
	case network.NetworkMsg_SignatureType:
		msg.MsgType = core.SSVPostConsensusMsgType
	case network.NetworkMsg_IBFTType:
		fallthrough
	case network.NetworkMsg_DecidedType:
		msg.MsgType = core.SSVConsensusMsgType
	}

	if msgV0.SignedMessage != nil {
		data, err := json.Marshal(msgV0.SignedMessage)
		if err != nil {
			return nil, err
		}
		msg.Data = data
		if msgV0.SignedMessage.Message != nil {
			msg.ID = msgV0.SignedMessage.Message.Lambda
		}
	}

	return &msg, nil
}

// ToNetworkV0 converts a v0 message to v1
func ToNetworkV0(msg *core.SSVMessage) (*network.Message, error) {
	msgV0 := network.Message{}

	switch msg.GetType() {
	case core.SSVConsensusMsgType:
		msgV0.Type = network.NetworkMsg_IBFTType
	case core.SSVPostConsensusMsgType:
		msgV0.Type = network.NetworkMsg_SignatureType
	case core.SSVSyncMsgType:
		msgV0.Type = network.NetworkMsg_SyncType
		var parsed network.SyncMessage
		err := json.Unmarshal(msg.Data, &parsed)
		if err != nil {
			return nil, err
		}
		msgV0.SyncMessage = &parsed
		// TODO: stream ID?
		return &msgV0, nil
	}

	var parsed proto.SignedMessage
	err := json.Unmarshal(msg.Data, &parsed)
	if err != nil {
		return nil, err
	}
	msgV0.SignedMessage = &parsed

	if m := msgV0.SignedMessage.GetMessage(); m != nil && m.GetType() == proto.RoundState_Decided {
		msgV0.Type = network.NetworkMsg_DecidedType
	}

	return &msgV0, nil
}
