package stubdkg

import (
	"encoding/json"
	"github.com/bloxapp/ssv/spec/types"
	"github.com/herumi/bls-eth-go-binary/bls"
)

type stage int

const (
	stubStage1 stage = iota
	stubStage2
	stubStage3
)

type protocolMsg struct {
	Stage  stage
	Points map[types.OperatorID]*bls.Fr // TODO this needs to be encrypted somehow
}

// Encode returns a msg encoded bytes or error
func (msg *protocolMsg) Encode() ([]byte, error) {
	return json.Marshal(msg)
}

// Decode returns error if decoding failed
func (msg *protocolMsg) Decode(data []byte) error {
	return json.Unmarshal(data, msg)
}
