package v0

import (
	"github.com/bloxapp/ssv/network"
	"github.com/bloxapp/ssv/protocol/v1"
)

// EncodeNetworkMsg - genesis version 0
func (v0 *ForkV0) EncodeNetworkMsg(msg v1.MessageEncoder) ([]byte, error) {
	return msg.Encode()
}

// DecodeNetworkMsg - genesis version 0
func (v0 *ForkV0) DecodeNetworkMsg(data []byte) (v1.MessageEncoder, error) {
	msg := network.Message{}
	err := msg.Decode(data)
	if err != nil {
		return nil, err
	}
	return &msg, nil
}
