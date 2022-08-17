package genesis

import (
	spectypes "github.com/bloxapp/ssv-spec/types"
)

// EncodeNetworkMsg encodes network message
func (g *ForkGenesis) EncodeNetworkMsg(msg *spectypes.SSVMessage) ([]byte, error) {
	return msg.Encode()
}

// DecodeNetworkMsg decodes network message
func (g *ForkGenesis) DecodeNetworkMsg(data []byte) (*spectypes.SSVMessage, error) {
	msg := spectypes.SSVMessage{}
	err := msg.Decode(data)
	if err != nil {
		return nil, err
	}
	return &msg, nil
}
