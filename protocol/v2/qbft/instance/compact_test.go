package instance

import (
	"testing"

	specqbft "github.com/ssvlabs/ssv-spec-pre-cc/qbft"
	spectypes "github.com/ssvlabs/ssv-spec-pre-cc/types"
	"github.com/stretchr/testify/require"
)

var compactTests = []struct {
	name       string
	inputState *specqbft.State
	inputMsg   *specqbft.SignedMessage
	expected   *specqbft.State // if nil, expected to be equal to input
}{
	{
		name:       "empty",
		inputState: &specqbft.State{},
		expected:   nil,
	},
	{
		name: "empty but not nil",
		inputState: &specqbft.State{
			Round:                1,
			ProposeContainer:     &specqbft.MsgContainer{},
			PrepareContainer:     &specqbft.MsgContainer{},
			CommitContainer:      &specqbft.MsgContainer{},
			RoundChangeContainer: &specqbft.MsgContainer{},
		},
		expected: nil,
	},
	{
		name: "nothing to compact",
		inputState: &specqbft.State{
			Round:                1,
			ProposeContainer:     mockContainer(1, 2),
			PrepareContainer:     mockContainer(1, 2),
			CommitContainer:      mockContainer(1, 2),
			RoundChangeContainer: mockContainer(1, 2),
		},
		expected: nil,
	},
	{
		name: "compact non-decided with previous rounds",
		inputState: &specqbft.State{
			Round:                2,
			LastPreparedRound:    2,
			ProposeContainer:     mockContainer(1, 2),
			PrepareContainer:     mockContainer(1, 2),
			CommitContainer:      mockContainer(1, 2),
			RoundChangeContainer: mockContainer(1, 2),
		},
		expected: &specqbft.State{
			Round:                2,
			LastPreparedRound:    2,
			ProposeContainer:     mockContainer(2),
			PrepareContainer:     mockContainer(2),
			CommitContainer:      mockContainer(2),
			RoundChangeContainer: mockContainer(2),
		},
	},
	{
		name: "compact non-decided with previous rounds except for prepared",
		inputState: &specqbft.State{
			Round:                2,
			LastPreparedRound:    1,
			ProposeContainer:     mockContainer(1, 2),
			PrepareContainer:     mockContainer(1, 2),
			CommitContainer:      mockContainer(1, 2),
			RoundChangeContainer: mockContainer(1, 2),
		},
		expected: &specqbft.State{
			Round:                2,
			LastPreparedRound:    1,
			ProposeContainer:     mockContainer(2),
			PrepareContainer:     mockContainer(1, 2),
			CommitContainer:      mockContainer(2),
			RoundChangeContainer: mockContainer(2),
		},
	},
	{
		name: "compact quorum decided with previous rounds",
		inputState: &specqbft.State{
			Round:             3,
			LastPreparedRound: 3,
			Decided:           true,
			Share: &spectypes.Share{
				Committee: make([]*spectypes.Operator, 4),
			},
			ProposeContainer:     mockContainer(1, 2, 3, 4),
			PrepareContainer:     mockContainer(1, 2, 3, 4),
			CommitContainer:      mockContainer(1, 2, 3, 4),
			RoundChangeContainer: mockContainer(1, 2, 3, 4),
		},
		inputMsg: &specqbft.SignedMessage{
			Signers: []spectypes.OperatorID{1, 2, 3},
		},
		expected: &specqbft.State{
			Round:             3,
			LastPreparedRound: 3,
			Decided:           true,
			Share: &spectypes.Share{
				Committee: make([]*spectypes.Operator, 4),
			},
			ProposeContainer:     mockContainer(),
			PrepareContainer:     mockContainer(),
			CommitContainer:      mockContainer(3, 4),
			RoundChangeContainer: mockContainer(),
		},
	},
	{
		name: "compact whole committee decided with previous rounds",
		inputState: &specqbft.State{
			Round:             2,
			LastPreparedRound: 2,
			Decided:           true,
			Share: &spectypes.Share{
				Committee: make([]*spectypes.Operator, 4),
			},
			ProposeContainer:     mockContainer(1, 2, 3, 4),
			PrepareContainer:     mockContainer(1, 2, 3, 4),
			CommitContainer:      mockContainer(1, 2, 3, 4),
			RoundChangeContainer: mockContainer(1, 2, 3, 4),
		},
		inputMsg: &specqbft.SignedMessage{
			Signers: []spectypes.OperatorID{1, 2, 3, 4},
		},
		expected: &specqbft.State{
			Round:             2,
			LastPreparedRound: 2,
			Decided:           true,
			Share: &spectypes.Share{
				Committee: make([]*spectypes.Operator, 4),
			},
			ProposeContainer:     mockContainer(),
			PrepareContainer:     mockContainer(),
			CommitContainer:      mockContainer(2, 3, 4),
			RoundChangeContainer: mockContainer(),
		},
	},
}

func TestCompact(t *testing.T) {
	for _, tt := range compactTests {
		t.Run(tt.name, func(t *testing.T) {
			inputStateBefore, err := tt.inputState.Encode()
			require.NoError(t, err)

			if tt.expected == nil {
				tt.expected = &specqbft.State{}
				require.NoError(t, tt.expected.Decode(inputStateBefore))
			}

			// Test CompactCopy.
			stateCopy := CompactCopy(tt.inputState, tt.inputMsg)
			require.Equal(t, tt.expected, stateCopy)

			// Verify that input state was not modified by CompactCopy.
			inputStateAfter, err := tt.inputState.Encode()
			require.NoError(t, err)
			require.Equal(t, inputStateBefore, inputStateAfter)

			// Test Compact.
			Compact(tt.inputState, tt.inputMsg)
			require.Equal(t, tt.expected, tt.inputState)
		})
	}
}

func mockContainer(rounds ...specqbft.Round) *specqbft.MsgContainer {
	container := specqbft.NewMsgContainer()
	for _, round := range rounds {
		container.AddMsg(&specqbft.SignedMessage{
			Message: specqbft.Message{
				Round: round,
			},
		})
	}
	return container
}
