package v2

import (
	"github.com/bloxapp/ssv/protocol/v1/message"
)

// EncodeNetworkMsg encodes network message
func (v2 *ForkV2) EncodeNetworkMsg(msg *message.SSVMessage) ([]byte, error) {
	return msg.Encode()
}

// DecodeNetworkMsg decodes network message
func (v2 *ForkV2) DecodeNetworkMsg(data []byte) (*message.SSVMessage, error) {
	msg := message.SSVMessage{}
	err := msg.Decode(data)
	if err != nil {
		return nil, err
	}
	return &msg, nil
}
