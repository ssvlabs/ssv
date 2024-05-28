package types

import (
	spectypes "github.com/ssvlabs/ssv-spec/types"
	"github.com/ssvlabs/ssv/operator/keys"
)

type SsvOperatorSigner struct {
	keys.OperatorSigner
	GetOperatorIdF func() spectypes.OperatorID
}

func NewSsvOperatorSigner(pk keys.OperatorPrivateKey, getOperatorId func() spectypes.OperatorID) *SsvOperatorSigner {
	return &SsvOperatorSigner{
		OperatorSigner: pk,
		GetOperatorIdF: getOperatorId,
	}
}

func (s *SsvOperatorSigner) GetOperatorID() spectypes.OperatorID {
	return s.GetOperatorIdF()
}

func (s *SsvOperatorSigner) SignSSVMessage(ssvMsg *spectypes.SSVMessage) ([]byte, error) {
	encodedMsg, err := ssvMsg.Encode()
	if err != nil {
		return nil, err
	}

	return s.Sign(encodedMsg)
}
