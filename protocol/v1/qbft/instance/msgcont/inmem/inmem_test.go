package inmem

import (
	"testing"

	specqbft "github.com/bloxapp/ssv-spec/qbft"
	spectypes "github.com/bloxapp/ssv-spec/types"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	"github.com/bloxapp/ssv/protocol/v1/message"
	protocoltesting "github.com/bloxapp/ssv/protocol/v1/testing"
	"github.com/bloxapp/ssv/utils/logex"
)

func init() {
	logex.Build("test", zap.InfoLevel, nil)
}

func generateConsensusMsg(r specqbft.Round) *specqbft.Message {
	return &specqbft.Message{
		MsgType:    specqbft.RoundChangeMsgType,
		Height:     1,
		Round:      r,
		Identifier: message.NewIdentifier([]byte("pk"), spectypes.BNRoleAttester),
		Data:       nil,
	}
}

func TestFindPartialChangeRound(t *testing.T) {
	uids := []spectypes.OperatorID{1, 2, 3, 4}
	secretKeys, _ := protocoltesting.GenerateBLSKeys(uids...)

	tests := []struct {
		name           string
		msgs           []*specqbft.SignedMessage
		expectedFound  bool
		expectedLowest uint64
	}{
		{
			"lowest 4",
			[]*specqbft.SignedMessage{
				protocoltesting.SignMsg(t, secretKeys, uids[0:1], generateConsensusMsg(4)),
				protocoltesting.SignMsg(t, secretKeys, uids[1:2], generateConsensusMsg(7)),
			},
			true,
			4,
		},
		{
			"lowest is lower than state round",
			[]*specqbft.SignedMessage{
				protocoltesting.SignMsg(t, secretKeys, uids[0:1], generateConsensusMsg(1)),
				protocoltesting.SignMsg(t, secretKeys, uids[1:2], generateConsensusMsg(0)),
			},
			false,
			100000,
		},
		{
			"lowest 7",
			[]*specqbft.SignedMessage{
				protocoltesting.SignMsg(t, secretKeys, uids[0:1], generateConsensusMsg(7)),
				protocoltesting.SignMsg(t, secretKeys, uids[1:2], generateConsensusMsg(9)),
				protocoltesting.SignMsg(t, secretKeys, uids[2:3], generateConsensusMsg(10)),
			},
			true,
			7,
		},
		{
			"not found",
			[]*specqbft.SignedMessage{},
			false,
			100000,
		},
		{
			"duplicate msgs from same peer, no quorum",
			[]*specqbft.SignedMessage{
				protocoltesting.SignMsg(t, secretKeys, uids[0:1], generateConsensusMsg(4)),
				protocoltesting.SignMsg(t, secretKeys, uids[0:1], generateConsensusMsg(5)),
			},
			false,
			4,
		},
		{
			"duplicate msgs from same peer, lowest 8",
			[]*specqbft.SignedMessage{
				protocoltesting.SignMsg(t, secretKeys, uids[0:1], generateConsensusMsg(13)),
				protocoltesting.SignMsg(t, secretKeys, uids[0:1], generateConsensusMsg(12)),
				protocoltesting.SignMsg(t, secretKeys, uids[1:2], generateConsensusMsg(10)),
				protocoltesting.SignMsg(t, secretKeys, uids[1:2], generateConsensusMsg(8)),
			},
			true,
			8,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(tt *testing.T) {
			c := New(3, 2)
			for _, msg := range test.msgs {
				c.AddMessage(msg, nil)
			}

			found, lowest := c.PartialChangeRoundQuorum(1)
			require.EqualValues(tt, test.expectedFound, found)
			require.EqualValues(tt, test.expectedLowest, lowest)
		})
	}
}

func TestMessagesContainer_OverrideMessages(t *testing.T) {
	c := New(3, 2)
	c.AddMessage(&specqbft.SignedMessage{
		Message: &specqbft.Message{
			Round:      1,
			Identifier: nil,
			Data:       []byte{1, 1, 1, 1},
		},
		Signature: nil,
		Signers:   []spectypes.OperatorID{1, 4},
	}, nil)
	c.AddMessage(&specqbft.SignedMessage{
		Message: &specqbft.Message{
			Round:      1,
			Identifier: nil,
			Data:       []byte{1, 1, 1, 1},
		},
		Signature: nil,
		Signers:   []spectypes.OperatorID{2, 3},
	}, nil)

	c.OverrideMessages(&specqbft.SignedMessage{
		Message: &specqbft.Message{
			Round:      1,
			Identifier: nil,
			Data:       []byte{1, 1, 1, 1},
		},
		Signature: nil,
		Signers:   []spectypes.OperatorID{1},
	}, nil)
	require.Len(t, c.ReadOnlyMessagesByRound(1), 1)
}

func TestMessagesContainer_AddMessage(t *testing.T) {
	c := New(3, 2)
	c.AddMessage(&specqbft.SignedMessage{
		Message: &specqbft.Message{
			Round:      1,
			Identifier: nil,
			Data:       []byte{1, 1, 1, 1},
		},
		Signature: nil,
		Signers:   []spectypes.OperatorID{1, 2, 3, 4},
	}, nil)

	require.Len(t, c.ReadOnlyMessagesByRound(1), 1)
	require.Len(t, c.ReadOnlyMessagesByRound(2), 0)

	// try to add duplicate
	c.AddMessage(&specqbft.SignedMessage{
		Message: &specqbft.Message{
			Round:      1,
			Identifier: nil,
			Data:       []byte{1, 1, 1, 1},
		},
		Signature: nil,
		Signers:   []spectypes.OperatorID{4, 5},
	}, nil)
	require.Len(t, c.ReadOnlyMessagesByRound(1), 1)
	require.Len(t, c.ReadOnlyMessagesByRound(2), 0)
	c.AddMessage(&specqbft.SignedMessage{
		Message: &specqbft.Message{
			Round:      1,
			Identifier: nil,
			Data:       []byte{1, 1, 1, 1},
		},
		Signature: nil,
		Signers:   []spectypes.OperatorID{4},
	}, nil)
	require.Len(t, c.ReadOnlyMessagesByRound(1), 1)
	require.Len(t, c.ReadOnlyMessagesByRound(2), 0)
}

func TestMessagesContainer_ReadOnlyMessagesByRound(t *testing.T) {
	c := New(3, 2)
	c.AddMessage(&specqbft.SignedMessage{
		Message: &specqbft.Message{
			Round:      1,
			Identifier: nil,
			Data:       []byte{1, 1, 1, 1},
		},
		Signature: nil,
		Signers:   []spectypes.OperatorID{1, 2, 3, 4},
	}, nil)
	c.AddMessage(&specqbft.SignedMessage{
		Message: &specqbft.Message{
			Round:      1,
			Identifier: nil,
			Data:       []byte{1, 1, 1, 1},
		},
		Signature: nil,
		Signers:   []spectypes.OperatorID{5},
	}, nil)

	msgs := c.ReadOnlyMessagesByRound(1)
	require.EqualValues(t, 1, msgs[0].Message.Round)
	require.EqualValues(t, 1, msgs[1].Message.Round)
	require.EqualValues(t, []byte{1, 1, 1, 1}, msgs[0].Message.Data)
	require.EqualValues(t, []byte{1, 1, 1, 1}, msgs[1].Message.Data)
	require.EqualValues(t, []spectypes.OperatorID{1, 2, 3, 4}, msgs[0].Signers)
	require.EqualValues(t, []spectypes.OperatorID{5}, msgs[1].Signers)
}

func TestMessagesContainer_QuorumAchieved(t *testing.T) {
	c := New(3, 2)
	c.AddMessage(&specqbft.SignedMessage{
		Message: &specqbft.Message{
			Round:      1,
			Identifier: nil,
			Data:       []byte{1, 1, 1, 1},
		},
		Signature: nil,
		Signers:   []spectypes.OperatorID{1, 2, 3},
	}, []byte{1, 1, 1, 1})
	res, _ := c.QuorumAchieved(1, []byte{1, 1, 1, 1})
	require.True(t, res)
	res, _ = c.QuorumAchieved(0, []byte{1, 1, 1, 1})
	require.False(t, res)
	res, _ = c.QuorumAchieved(1, []byte{1, 1, 1, 0})
	require.False(t, res)

	c.AddMessage(&specqbft.SignedMessage{
		Message: &specqbft.Message{
			Round:      2,
			Identifier: nil,
			Data:       []byte{1, 1, 1, 1},
		},
		Signature: nil,
		Signers:   []spectypes.OperatorID{1, 2},
	}, []byte{1, 1, 1, 1})
	res, _ = c.QuorumAchieved(2, []byte{1, 1, 1, 1})
	require.False(t, res)
	c.AddMessage(&specqbft.SignedMessage{
		Message: &specqbft.Message{
			Round:      2,
			Identifier: nil,
			Data:       []byte{1, 1, 1, 1},
		},
		Signature: nil,
		Signers:   []spectypes.OperatorID{3},
	}, []byte{1, 1, 1, 1})
	res, _ = c.QuorumAchieved(2, []byte{1, 1, 1, 1})
	require.True(t, res)
	res, _ = c.QuorumAchieved(3, []byte{1, 1, 1, 1})
	require.False(t, res)
	res, _ = c.QuorumAchieved(2, []byte{1, 1, 1, 0})
	require.False(t, res)
}
