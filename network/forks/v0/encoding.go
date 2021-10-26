package v0

import (
	"encoding/json"
	"github.com/bloxapp/ssv/network"
)

// EncodeNetworkMsg - genesis version 0
func (v0 *ForkV0) EncodeNetworkMsg(msg *network.Message) ([]byte, error) {
	return json.Marshal(msg)
}

// DecodeNetworkMsg - genesis version 0
func (v0 *ForkV0) DecodeNetworkMsg(data []byte) (*network.Message, error) {
	ret := &network.Message{}
	err := json.Unmarshal(data, ret)
	return ret, err
}
