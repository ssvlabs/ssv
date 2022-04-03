package v0

import (
	"github.com/bloxapp/ssv/network"
	"github.com/bloxapp/ssv/protocol/v1/message"
)

// EncodeNetworkMsg - genesis version 0
func (v0 *ForkV0) EncodeNetworkMsg(msg message.Encoder) ([]byte, error) {
	v1Msg, ok := msg.(*message.SSVMessage)
	if !ok {
		// already v0 probably
		return msg.Encode()
	}
	v0Msg, err := ToV0Message(v1Msg)
	if err != nil {
		return nil, err
	}
	return v0Msg.Encode()
}

// DecodeNetworkMsg - genesis version 0
func (v0 *ForkV0) DecodeNetworkMsg(data []byte) (message.Encoder, error) {
	v0Msg := &network.Message{}
	err := v0Msg.Decode(data)
	if err != nil {
		return nil, err
	}
	return ToV1Message(v0Msg)
}
