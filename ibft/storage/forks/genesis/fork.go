package genesis

import (
	specqbft "github.com/bloxapp/ssv-spec/qbft"
)

// ForkGenesis is the genesis fork for controller
type ForkGenesis struct {
}

// EncodeSignedMsg encodes signed message
func (f ForkGenesis) EncodeSignedMsg(msg *specqbft.SignedMessage) ([]byte, error) {
	return msg.Encode()
}

// DecodeSignedMsg decodes signed message
func (f ForkGenesis) DecodeSignedMsg(data []byte) (*specqbft.SignedMessage, error) {
	msgV1 := &specqbft.SignedMessage{}
	err := msgV1.Decode(data)
	if err != nil {
		return nil, err
	}
	return msgV1, nil
}
