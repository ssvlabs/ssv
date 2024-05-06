package types

import (
	spectypes "github.com/bloxapp/ssv-spec/types"
	"github.com/bloxapp/ssv/operator/keys"
)

type SsvOperatorSigner struct {
	keys.OperatorSigner
	Id spectypes.OperatorID
}

func NewSsvOperatorSigner(pk keys.OperatorPrivateKey, id spectypes.OperatorID) *SsvOperatorSigner {
	return &SsvOperatorSigner{
		OperatorSigner: pk,
		Id:             id,
	}
}

func (s *SsvOperatorSigner) GetOperatorID() spectypes.OperatorID {
	return s.Id
}

func (s *SsvOperatorSigner) SignSSVMessage(ssvMsg *spectypes.SSVMessage) ([]byte, error) {
	encodedMsg, err := ssvMsg.Encode()
	if err != nil {
		return nil, err
	}

	return s.Sign(encodedMsg)
}
