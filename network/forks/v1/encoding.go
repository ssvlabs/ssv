package v1

import (
	"github.com/bloxapp/ssv/protocol/v1/message"
)

// EncodeNetworkMsg encodes network message
func (v1 *ForkV1) EncodeNetworkMsg(msg *message.SSVMessage) ([]byte, error) {
	return msg.Encode()
}

// DecodeNetworkMsg decodes network message
func (v1 *ForkV1) DecodeNetworkMsg(data []byte) (*message.SSVMessage, error) {
	msg := message.SSVMessage{}
	err := msg.Decode(data)
	if err != nil {
		return nil, err
	}
	return &msg, nil
}
