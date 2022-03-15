package v0

import (
	"encoding/json"
	"github.com/bloxapp/ssv/network"
	"github.com/bloxapp/ssv/protocol"
)

// TODO: change to SSZ encoding and clear v0

// EncodeNetworkMsg - genesis version 1
func (v1 *ForkV1) EncodeNetworkMsg(msg *network.Message) ([]byte, error) {
	return json.Marshal(msg)
}

// DecodeNetworkMsg - genesis version 1
func (v1 *ForkV1) DecodeNetworkMsg(data []byte) (*network.Message, error) {
	ret := &network.Message{}
	err := json.Unmarshal(data, ret)
	return ret, err
}

// EncodeNetworkMsgV1 encodes message v1
func (v1 *ForkV1) EncodeNetworkMsgV1(msg *protocol.SSVMessage) ([]byte, error) {
	return json.Marshal(msg)
}

// DecodeNetworkMsgV1 decodes message v1
func (v1 *ForkV1) DecodeNetworkMsgV1(data []byte) (*protocol.SSVMessage, error) {
	msg := protocol.SSVMessage{}
	err := json.Unmarshal(data, &msg)
	return &msg, err
}
