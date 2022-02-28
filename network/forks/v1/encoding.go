package v0

import (
	"encoding/json"
	"github.com/bloxapp/ssv/network"
)

// TODO: change to SSZ encoding

// EncodeNetworkMsg - genesis version 1
func (v1 *ForkV1) EncodeNetworkMsg(msg *network.Message) ([]byte, error) {
	return json.Marshal(msg)
}

// DecodeNetworkMsg - genesis version 1
func (v1 *ForkV1) DecodeNetworkMsg(data []byte) (*network.Message, error) {
	ret := &network.Message{}
	err := json.Unmarshal(data, ret)
	return ret, err
}
