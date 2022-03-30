package v1

import (
	"github.com/bloxapp/ssv/network"
	"github.com/bloxapp/ssv/protocol/v1/message"
)

// EncodeNetworkMsg encodes network message
func (v1 *ForkV1) EncodeNetworkMsg(msg message.Encoder) ([]byte, error) {
	v0Msg, ok := msg.(*network.Message)
	if !ok {
		//return nil, errors.New("could not convert message")
		// already v1
		return msg.Encode()
	}
	v1Msg, err := ToV1Message(v0Msg)
	if err != nil {
		return nil, err
	}
	return v1Msg.Encode()
}

// EncodeNetworkMsgV1 encodes network message
func (v1 *ForkV1) EncodeNetworkMsgV1(msg message.Encoder) ([]byte, error) {
	return msg.Encode()
}

// DecodeNetworkMsg decodes network message
func (v1 *ForkV1) DecodeNetworkMsg(data []byte) (message.Encoder, error) {
	msg := message.SSVMessage{}
	err := msg.Decode(data)
	if err != nil {
		return nil, err
	}
	v0Msg, err := ToV0Message(&msg)
	if err != nil {
		return nil, err
	}
	return v0Msg, nil
}

// DecodeNetworkMsgV1 decodes network message and returns the actual struct
func (v1 *ForkV1) DecodeNetworkMsgV1(data []byte) (*message.SSVMessage, error) {
	msg := message.SSVMessage{}
	err := msg.Decode(data)
	if err != nil {
		return nil, err
	}
	return &msg, nil
}
