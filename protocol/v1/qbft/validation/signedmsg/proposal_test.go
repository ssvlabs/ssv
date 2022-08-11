package signedmsg

import (
	"testing"

	specqbft "github.com/bloxapp/ssv-spec/qbft"
	"github.com/stretchr/testify/require"

	"github.com/bloxapp/ssv/protocol/v1/qbft"
)

func TestProposalExists(t *testing.T) {
	tests := []struct {
		name                            string
		round                           specqbft.Round
		proposalAcceptedForCurrentRound *specqbft.SignedMessage
		expectedError                   string
	}{
		{
			"proposal exists",
			1,
			&specqbft.SignedMessage{
				Message: &specqbft.Message{
					Round: 1,
				},
			},
			"",
		},
		{
			"no proposal",
			1,
			nil,
			"did not receive proposal for this round",
		},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			state := &qbft.State{}
			state.ProposalAcceptedForCurrentRound.Store(tc.proposalAcceptedForCurrentRound)
			pipeline := ProposalExists(state)
			err := pipeline.Run(&specqbft.SignedMessage{
				Message: &specqbft.Message{
					Round: tc.round,
				},
			})

			if len(tc.expectedError) == 0 {
				require.NoError(t, err)
			} else {
				require.EqualError(t, err, tc.expectedError)
			}
		})
	}
}
