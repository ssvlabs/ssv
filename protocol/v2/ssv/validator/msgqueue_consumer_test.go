package validator

import (
	"testing"

	"github.com/bloxapp/ssv-spec/qbft"
	"github.com/bloxapp/ssv-spec/types"
	"github.com/bloxapp/ssv-spec/types/testingutils"
	"github.com/stretchr/testify/require"
)

func TestMessageConsumer(t *testing.T) {
	validator := testingutils.BaseValidator(testingutils.Testing4SharesSet())
	msg := testingutils.SSVMsgAttester(
		&qbft.SignedMessage{
			Message: &qbft.Message{
				MsgType:    qbft.CommitMsgType,
				Height:     100,
				Round:      2,
				Identifier: []byte{1, 2, 3, 4},
				Data:       []byte{1, 2, 3, 4},
			},
			Signature: []byte{1, 2, 3, 4},
			Signers:   make([]types.OperatorID, 4),
		},
		nil,
	)
	err := validator.ProcessMessage(msg)
	require.NoError(t, err)
}
