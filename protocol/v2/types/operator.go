package types

import (
	"github.com/pkg/errors"
	"github.com/ssvlabs/ssv-spec/qbft"
	spectypes "github.com/ssvlabs/ssv-spec/types"

	"github.com/ssvlabs/ssv/ssvsigner/keys"
)

func OperatorIDsFromOperators(operators []*spectypes.Operator) []spectypes.OperatorID {
	ids := make([]spectypes.OperatorID, len(operators))
	for i, op := range operators {
		ids[i] = op.OperatorID
	}
	return ids
}

// OperatorSigner used to sign protocol messages.
type OperatorSigner interface {
	SignSSVMessage(ssvMsg *spectypes.SSVMessage) ([]byte, error)
	GetOperatorID() spectypes.OperatorID
}

type SsvOperatorSigner struct {
	keys.OperatorSigner
	GetOperatorIdF func() spectypes.OperatorID
}

func NewSsvOperatorSigner(operatorSigner keys.OperatorSigner, getOperatorId func() spectypes.OperatorID) *SsvOperatorSigner {
	return &SsvOperatorSigner{
		OperatorSigner: operatorSigner,
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

func Sign(msg *qbft.Message, operatorID spectypes.OperatorID, operatorSigner OperatorSigner) (*spectypes.SignedSSVMessage, error) {
	byts, err := msg.Encode()
	if err != nil {
		return nil, errors.Wrap(err, "could not encode message")
	}

	msgID := spectypes.MessageID{}
	copy(msgID[:], msg.Identifier)

	ssvMsg := &spectypes.SSVMessage{
		MsgType: spectypes.SSVConsensusMsgType,
		MsgID:   msgID,
		Data:    byts,
	}

	sig, err := operatorSigner.SignSSVMessage(ssvMsg)
	if err != nil {
		return nil, errors.Wrap(err, "could not sign SSVMessage")
	}

	signedSSVMessage := &spectypes.SignedSSVMessage{
		Signatures:  [][]byte{sig},
		OperatorIDs: []spectypes.OperatorID{operatorID},
		SSVMessage:  ssvMsg,
	}

	return signedSSVMessage, nil
}
