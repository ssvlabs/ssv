package v0

import (
	"github.com/bloxapp/ssv/network"
	"github.com/bloxapp/ssv/protocol"
)

// EncodeNetworkMsg - genesis version 0
func (v0 *ForkV0) EncodeNetworkMsg(msg protocol.MessageEncoder) ([]byte, error) {
	return msg.Encode()
}

// DecodeNetworkMsg - genesis version 0
func (v0 *ForkV0) DecodeNetworkMsg(data []byte) (protocol.MessageEncoder, error) {
	msg := network.Message{}
	err := msg.Decode(data)
	if err != nil {
		return nil, err
	}
	return &msg, nil
}

// EncodeNetworkMsgV1 encodes message v1
func (v0 *ForkV0) EncodeNetworkMsgV1(msg *protocol.SSVMessage) ([]byte, error) {
	return json.Marshal(msg)
}

// DecodeNetworkMsgV1 decodes message v1
func (v0 *ForkV0) DecodeNetworkMsgV1(data []byte) (*protocol.SSVMessage, error) {
	msg := protocol.SSVMessage{}
	err := json.Unmarshal(data, &msg)
	return &msg, err
}
