package v0

import (
	"github.com/bloxapp/ssv/network"
	"github.com/bloxapp/ssv/protocol/v1/message"
)

// EncodeNetworkMsg converts the message to v0 and encodes it
func (v0 *ForkV0) EncodeNetworkMsg(msg *message.SSVMessage) ([]byte, error) {
	v0Msg, err := ToV0Message(msg)
	if err != nil {
		return nil, err
	}
	return v0Msg.Encode()
}

// DecodeNetworkMsg decodes network message and converts it to v1
func (v0 *ForkV0) DecodeNetworkMsg(data []byte) (*message.SSVMessage, error) {
	v0Msg := &network.Message{}
	err := v0Msg.Decode(data)
	if err != nil {
		return nil, err
	}
	return ToV1Message(v0Msg)
}
