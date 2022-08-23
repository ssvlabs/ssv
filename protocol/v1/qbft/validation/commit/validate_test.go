package commit

import (
	"testing"

	specqbft "github.com/bloxapp/ssv-spec/qbft"
	spectypes "github.com/bloxapp/ssv-spec/types"
	"github.com/stretchr/testify/require"

	"github.com/bloxapp/ssv/protocol/v1/qbft"
)

func TestValidateProposal(t *testing.T) {
	data1, err := (&specqbft.CommitData{Data: []byte("data1")}).Encode()
	require.NoError(t, err)

	data2, err := (&specqbft.CommitData{Data: []byte("data2")}).Encode()
	require.NoError(t, err)

	tests := []struct {
		name                            string
		msg                             *specqbft.SignedMessage
		proposalAcceptedForCurrentRound *specqbft.SignedMessage
		err                             string
	}{
		{
			"proposal has same value",
			&specqbft.SignedMessage{
				Message: &specqbft.Message{
					MsgType:    specqbft.CommitMsgType,
					Round:      1,
					Identifier: []byte("Lambda"),
					Data:       data1,
				},
				Signature: []byte{},
				Signers:   []spectypes.OperatorID{},
			},
			&specqbft.SignedMessage{
				Message: &specqbft.Message{
					MsgType:    specqbft.CommitMsgType,
					Round:      1,
					Identifier: []byte("Lambda"),
					Data:       data1,
				},
				Signature: []byte{},
				Signers:   []spectypes.OperatorID{},
			},
			"",
		},
		{
			"proposal has different value",
			&specqbft.SignedMessage{
				Message: &specqbft.Message{
					MsgType:    specqbft.CommitMsgType,
					Round:      1,
					Identifier: []byte("Lambda"),
					Data:       data1,
				},
				Signature: []byte{},
				Signers:   []spectypes.OperatorID{},
			},
			&specqbft.SignedMessage{
				Message: &specqbft.Message{
					MsgType:    specqbft.CommitMsgType,
					Round:      1,
					Identifier: []byte("Lambda"),
					Data:       data2,
				},
				Signature: []byte{},
				Signers:   []spectypes.OperatorID{},
			},
			"message data is different from proposed data",
		},
		{
			"no proposal",
			&specqbft.SignedMessage{
				Message: &specqbft.Message{
					MsgType:    specqbft.CommitMsgType,
					Round:      1,
					Identifier: []byte("Lambda"),
					Data:       data1,
				},
				Signature: []byte{},
				Signers:   []spectypes.OperatorID{},
			},
			nil,
			"did not receive proposal for this round",
		},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			state := &qbft.State{}
			state.ProposalAcceptedForCurrentRound.Store(tc.proposalAcceptedForCurrentRound)

			err := ValidateProposal(state).Run(tc.msg)
			if len(tc.err) > 0 {
				require.EqualError(t, err, tc.err)
			} else {
				require.NoError(t, err)
			}
		})
	}
}
