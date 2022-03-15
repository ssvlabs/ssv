package v1

import (
	"errors"
	"github.com/bloxapp/ssv/protocol"
)

// EncodeNetworkMsg encodes network message
func (v1 *ForkV1) EncodeNetworkMsg(msg protocol.MessageEncoder) ([]byte, error) {
	return msg.Encode()
}

// DecodeNetworkMsg decodes network message
func (v1 *ForkV1) DecodeNetworkMsg(data []byte) (protocol.MessageEncoder, error) {
	msg := protocol.SSVMessage{}
	err := msg.Decode(data)
	if err != nil {
		return nil, err
	}
	return &msg, nil
}

// DecodeNetworkMsgV1 decodes network message and returns the actual struct
func (v1 *ForkV1) DecodeNetworkMsgV1(data []byte) (*protocol.SSVMessage, error) {
	raw, err := v1.DecodeNetworkMsg(data)
	if err != nil {
		return nil, err
	}
	msg, ok := raw.(*protocol.SSVMessage)
	if !ok {
		return nil, errors.New("could not convert message")
	}
	return msg, nil
}
