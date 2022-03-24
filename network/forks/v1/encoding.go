package v1

import (
	"errors"
	"github.com/bloxapp/ssv/protocol/v1"
)

// EncodeNetworkMsg encodes network message
func (v1 *ForkV1) EncodeNetworkMsg(msg v1.MessageEncoder) ([]byte, error) {
	return msg.Encode()
}

// DecodeNetworkMsg decodes network message
func (v1 *ForkV1) DecodeNetworkMsg(data []byte) (v1.MessageEncoder, error) {
	msg := v1.SSVMessage{}
	err := msg.Decode(data)
	if err != nil {
		return nil, err
	}
	return &msg, nil
}

// DecodeNetworkMsgV1 decodes network message and returns the actual struct
func (v1 *ForkV1) DecodeNetworkMsgV1(data []byte) (*v1.SSVMessage, error) {
	raw, err := v1.DecodeNetworkMsg(data)
	if err != nil {
		return nil, err
	}
	msg, ok := raw.(*v1.SSVMessage)
	if !ok {
		return nil, errors.New("could not convert message")
	}
	return msg, nil
}
