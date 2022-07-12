package genesis

import (
	"github.com/bloxapp/ssv/protocol/v1/message"
)

// EncodeNetworkMsg encodes network message
func (g *ForkGenesis) EncodeNetworkMsg(msg *message.SSVMessage) ([]byte, error) {
	return msg.Encode()
}

// DecodeNetworkMsg decodes network message
func (g *ForkGenesis) DecodeNetworkMsg(data []byte) (*message.SSVMessage, error) {
	msg := message.SSVMessage{}
	err := msg.Decode(data)
	if err != nil {
		return nil, err
	}
	return &msg, nil
}
