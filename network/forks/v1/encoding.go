package v1

import (
	"github.com/bloxapp/ssv/network"
)

// EncodeNetworkMsg - version 1 implementation
func (v1 *ForkV1) EncodeNetworkMsg(msg *network.Message) ([]byte, error) {
	return v1.forkV0.EncodeNetworkMsg(msg)
}

// DecodeNetworkMsg - version 1 implementation
func (v1 *ForkV1) DecodeNetworkMsg(data []byte) (*network.Message, error) {
	return v1.forkV0.DecodeNetworkMsg(data)
}
