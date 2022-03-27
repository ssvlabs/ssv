package v1

import (
	"errors"
	"github.com/bloxapp/ssv/protocol/v1/core"
)

// EncodeNetworkMsg encodes network message
func (v1 *ForkV1) EncodeNetworkMsg(msg core.MessageEncoder) ([]byte, error) {
	return msg.Encode()
}

// DecodeNetworkMsg decodes network message
func (v1 *ForkV1) DecodeNetworkMsg(data []byte) (core.MessageEncoder, error) {
	msg := core.SSVMessage{}
	err := msg.Decode(data)
	if err != nil {
		return nil, err
	}
	return &msg, nil
}

// DecodeNetworkMsgV1 decodes network message and returns the actual struct
func (v1 *ForkV1) DecodeNetworkMsgV1(data []byte) (*core.SSVMessage, error) {
	raw, err := v1.DecodeNetworkMsg(data)
	if err != nil {
		return nil, err
	}
	msg, ok := raw.(*core.SSVMessage)
	if !ok {
		return nil, errors.New("could not convert message")
	}
	return msg, nil
}
