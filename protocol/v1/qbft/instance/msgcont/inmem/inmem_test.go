package inmem

import (
	testing2 "github.com/bloxapp/ssv/protocol/v1/testing"
	"testing"

	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	"github.com/bloxapp/ssv/protocol/v1/message"
	"github.com/bloxapp/ssv/utils/logex"
)

func init() {
	logex.Build("test", zap.InfoLevel, nil)
}

func generateConsensusMsg(r message.Round) *message.ConsensusMessage {
	return &message.ConsensusMessage{
		MsgType:    message.RoundChangeMsgType,
		Height:     1,
		Round:      r,
		Identifier: message.NewIdentifier([]byte("pk"), message.RoleTypeAttester),
		Data:       nil,
	}
}

func TestFindPartialChangeRound(t *testing.T) {
	uids := []message.OperatorID{1, 2, 3, 4}
	secretKeys, _ := testing2.GenerateBLSKeys(uids...)

	tests := []struct {
		name           string
		msgs           []*message.SignedMessage
		expectedFound  bool
		expectedLowest uint64
	}{
		{
			"lowest 4",
			[]*message.SignedMessage{
				testing2.SignMsg(t, secretKeys, uids[0:1], generateConsensusMsg(4)),
				testing2.SignMsg(t, secretKeys, uids[1:2], generateConsensusMsg(7)),
			},
			true,
			4,
		},
		{
			"lowest is lower than state round",
			[]*message.SignedMessage{
				testing2.SignMsg(t, secretKeys, uids[0:1], generateConsensusMsg(1)),
				testing2.SignMsg(t, secretKeys, uids[1:2], generateConsensusMsg(0)),
			},
			false,
			100000,
		},
		{
			"lowest 7",
			[]*message.SignedMessage{
				testing2.SignMsg(t, secretKeys, uids[0:1], generateConsensusMsg(7)),
				testing2.SignMsg(t, secretKeys, uids[1:2], generateConsensusMsg(9)),
				testing2.SignMsg(t, secretKeys, uids[2:3], generateConsensusMsg(10)),
			},
			true,
			7,
		},
		{
			"not found",
			[]*message.SignedMessage{},
			false,
			100000,
		},
		{
			"duplicate msgs from same peer, no quorum",
			[]*message.SignedMessage{
				testing2.SignMsg(t, secretKeys, uids[0:1], generateConsensusMsg(4)),
				testing2.SignMsg(t, secretKeys, uids[0:1], generateConsensusMsg(5)),
			},
			false,
			4,
		},
		{
			"duplicate msgs from same peer, lowest 8",
			[]*message.SignedMessage{
				testing2.SignMsg(t, secretKeys, uids[0:1], generateConsensusMsg(13)),
				testing2.SignMsg(t, secretKeys, uids[0:1], generateConsensusMsg(12)),
				testing2.SignMsg(t, secretKeys, uids[1:2], generateConsensusMsg(10)),
				testing2.SignMsg(t, secretKeys, uids[1:2], generateConsensusMsg(8)),
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
	c.AddMessage(&message.SignedMessage{
		Message: &message.ConsensusMessage{
			Round:      1,
			Identifier: nil,
			Data:       []byte{1, 1, 1, 1},
		},
		Signature: nil,
		Signers:   []message.OperatorID{1, 4},
	}, nil)
	c.AddMessage(&message.SignedMessage{
		Message: &message.ConsensusMessage{
			Round:      1,
			Identifier: nil,
			Data:       []byte{1, 1, 1, 1},
		},
		Signature: nil,
		Signers:   []message.OperatorID{2, 3},
	}, nil)

	c.OverrideMessages(&message.SignedMessage{
		Message: &message.ConsensusMessage{
			Round:      1,
			Identifier: nil,
			Data:       []byte{1, 1, 1, 1},
		},
		Signature: nil,
		Signers:   []message.OperatorID{1},
	}, nil)
	require.Len(t, c.ReadOnlyMessagesByRound(1), 1)
}

func TestMessagesContainer_AddMessage(t *testing.T) {
	c := New(3, 2)
	c.AddMessage(&message.SignedMessage{
		Message: &message.ConsensusMessage{
			Round:      1,
			Identifier: nil,
			Data:       []byte{1, 1, 1, 1},
		},
		Signature: nil,
		Signers:   []message.OperatorID{1, 2, 3, 4},
	}, nil)

	require.Len(t, c.ReadOnlyMessagesByRound(1), 1)
	require.Len(t, c.ReadOnlyMessagesByRound(2), 0)

	// try to add duplicate
	c.AddMessage(&message.SignedMessage{
		Message: &message.ConsensusMessage{
			Round:      1,
			Identifier: nil,
			Data:       []byte{1, 1, 1, 1},
		},
		Signature: nil,
		Signers:   []message.OperatorID{4, 5},
	}, nil)
	require.Len(t, c.ReadOnlyMessagesByRound(1), 1)
	require.Len(t, c.ReadOnlyMessagesByRound(2), 0)
	c.AddMessage(&message.SignedMessage{
		Message: &message.ConsensusMessage{
			Round:      1,
			Identifier: nil,
			Data:       []byte{1, 1, 1, 1},
		},
		Signature: nil,
		Signers:   []message.OperatorID{4},
	}, nil)
	require.Len(t, c.ReadOnlyMessagesByRound(1), 1)
	require.Len(t, c.ReadOnlyMessagesByRound(2), 0)
}

func TestMessagesContainer_ReadOnlyMessagesByRound(t *testing.T) {
	c := New(3, 2)
	c.AddMessage(&message.SignedMessage{
		Message: &message.ConsensusMessage{
			Round:      1,
			Identifier: nil,
			Data:       []byte{1, 1, 1, 1},
		},
		Signature: nil,
		Signers:   []message.OperatorID{1, 2, 3, 4},
	}, nil)
	c.AddMessage(&message.SignedMessage{
		Message: &message.ConsensusMessage{
			Round:      1,
			Identifier: nil,
			Data:       []byte{1, 1, 1, 1},
		},
		Signature: nil,
		Signers:   []message.OperatorID{5},
	}, nil)

	msgs := c.ReadOnlyMessagesByRound(1)
	require.EqualValues(t, 1, msgs[0].Message.Round)
	require.EqualValues(t, 1, msgs[1].Message.Round)
	require.EqualValues(t, []byte{1, 1, 1, 1}, msgs[0].Message.Data)
	require.EqualValues(t, []byte{1, 1, 1, 1}, msgs[1].Message.Data)
	require.EqualValues(t, []message.OperatorID{1, 2, 3, 4}, msgs[0].Signers)
	require.EqualValues(t, []message.OperatorID{5}, msgs[1].Signers)
}

func TestMessagesContainer_QuorumAchieved(t *testing.T) {
	c := New(3, 2)
	c.AddMessage(&message.SignedMessage{
		Message: &message.ConsensusMessage{
			Round:      1,
			Identifier: nil,
			Data:       []byte{1, 1, 1, 1},
		},
		Signature: nil,
		Signers:   []message.OperatorID{1, 2, 3},
	}, []byte{1, 1, 1, 1})
	res, _ := c.QuorumAchieved(1, []byte{1, 1, 1, 1})
	require.True(t, res)
	res, _ = c.QuorumAchieved(0, []byte{1, 1, 1, 1})
	require.False(t, res)
	res, _ = c.QuorumAchieved(1, []byte{1, 1, 1, 0})
	require.False(t, res)

	c.AddMessage(&message.SignedMessage{
		Message: &message.ConsensusMessage{
			Round:      2,
			Identifier: nil,
			Data:       []byte{1, 1, 1, 1},
		},
		Signature: nil,
		Signers:   []message.OperatorID{1, 2},
	}, []byte{1, 1, 1, 1})
	res, _ = c.QuorumAchieved(2, []byte{1, 1, 1, 1})
	require.False(t, res)
	c.AddMessage(&message.SignedMessage{
		Message: &message.ConsensusMessage{
			Round:      2,
			Identifier: nil,
			Data:       []byte{1, 1, 1, 1},
		},
		Signature: nil,
		Signers:   []message.OperatorID{3},
	}, []byte{1, 1, 1, 1})
	res, _ = c.QuorumAchieved(2, []byte{1, 1, 1, 1})
	require.True(t, res)
	res, _ = c.QuorumAchieved(3, []byte{1, 1, 1, 1})
	require.False(t, res)
	res, _ = c.QuorumAchieved(2, []byte{1, 1, 1, 0})
	require.False(t, res)
}
