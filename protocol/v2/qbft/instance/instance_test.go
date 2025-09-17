package instance

import (
	"testing"

	specqbft "github.com/ssvlabs/ssv-spec/qbft"
	spectypes "github.com/ssvlabs/ssv-spec/types"
	"github.com/ssvlabs/ssv-spec/types/testingutils"
	"github.com/stretchr/testify/require"
)

func TestInstance_Marshaling(t *testing.T) {
	var TestingMessage = &specqbft.Message{
		MsgType:    specqbft.ProposalMsgType,
		Height:     specqbft.FirstHeight,
		Round:      specqbft.FirstRound,
		Identifier: testingutils.TestingIdentifier,
		Root:       testingutils.TestingQBFTRootData,
	}
	TestingSK := testingutils.Testing4SharesSet()
	testingSignedMsg := testingutils.ToProcessingMessage(func() *spectypes.SignedSSVMessage {
		return testingutils.SignQBFTMsg(TestingSK.OperatorKeys[1], 1, TestingMessage)
	}())
	i := &specqbft.Instance{
		State: &specqbft.State{
			CommitteeMember:                 testingutils.TestingCommitteeMember(TestingSK),
			ID:                              testingutils.TestingIdentifier,
			Round:                           1,
			Height:                          1,
			LastPreparedRound:               1,
			LastPreparedValue:               []byte{1, 2, 3, 4},
			ProposalAcceptedForCurrentRound: testingSignedMsg,
			Decided:                         false,
			DecidedValue:                    []byte{1, 2, 3, 4},

			ProposeContainer: &specqbft.MsgContainer{
				Msgs: map[specqbft.Round][]*specqbft.ProcessingMessage{
					1: {
						testingSignedMsg,
					},
				},
			},
			PrepareContainer: &specqbft.MsgContainer{
				Msgs: map[specqbft.Round][]*specqbft.ProcessingMessage{
					1: {
						testingSignedMsg,
					},
				},
			},
			CommitContainer: &specqbft.MsgContainer{
				Msgs: map[specqbft.Round][]*specqbft.ProcessingMessage{
					1: {
						testingSignedMsg,
					},
				},
			},
			RoundChangeContainer: &specqbft.MsgContainer{
				Msgs: map[specqbft.Round][]*specqbft.ProcessingMessage{
					1: {
						testingSignedMsg,
					},
				},
			},
		},
	}

	byts, err := i.Encode()
	require.NoError(t, err)

	decoded := &specqbft.Instance{}
	require.NoError(t, decoded.Decode(byts))

	bytsDecoded, err := decoded.Encode()
	require.NoError(t, err)
	require.EqualValues(t, byts, bytsDecoded)
}
