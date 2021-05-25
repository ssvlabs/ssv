package inmem

import (
	"github.com/bloxapp/ssv/ibft/proto"
	"github.com/stretchr/testify/require"
	"testing"
)

func TestMessagesContainer_AddMessage(t *testing.T) {
	c := New(3)
	c.AddMessage(&proto.SignedMessage{
		Message: &proto.Message{
			Round:  1,
			Lambda: nil,
			Value:  []byte{1, 1, 1, 1},
		},
		Signature: nil,
		SignerIds: []uint64{1, 2, 3, 4},
	})

	require.Len(t, c.ReadOnlyMessagesByRound(1), 1)
	require.Len(t, c.ReadOnlyMessagesByRound(2), 0)

	// try to add duplicate
	c.AddMessage(&proto.SignedMessage{
		Message: &proto.Message{
			Round:  1,
			Lambda: nil,
			Value:  []byte{1, 1, 1, 1},
		},
		Signature: nil,
		SignerIds: []uint64{4, 5},
	})
	require.Len(t, c.ReadOnlyMessagesByRound(1), 1)
	require.Len(t, c.ReadOnlyMessagesByRound(2), 0)
	c.AddMessage(&proto.SignedMessage{
		Message: &proto.Message{
			Round:  1,
			Lambda: nil,
			Value:  []byte{1, 1, 1, 1},
		},
		Signature: nil,
		SignerIds: []uint64{4},
	})
	require.Len(t, c.ReadOnlyMessagesByRound(1), 1)
	require.Len(t, c.ReadOnlyMessagesByRound(2), 0)
}

func TestMessagesContainer_ReadOnlyMessagesByRound(t *testing.T) {
	c := New(3)
	c.AddMessage(&proto.SignedMessage{
		Message: &proto.Message{
			Round:  1,
			Lambda: nil,
			Value:  []byte{1, 1, 1, 1},
		},
		Signature: nil,
		SignerIds: []uint64{1, 2, 3, 4},
	})
	c.AddMessage(&proto.SignedMessage{
		Message: &proto.Message{
			Round:  1,
			Lambda: nil,
			Value:  []byte{1, 1, 1, 1},
		},
		Signature: nil,
		SignerIds: []uint64{5},
	})

	msgs := c.ReadOnlyMessagesByRound(1)
	require.EqualValues(t, 1, msgs[0].Message.Round)
	require.EqualValues(t, 1, msgs[1].Message.Round)
	require.EqualValues(t, []byte{1, 1, 1, 1}, msgs[0].Message.Value)
	require.EqualValues(t, []byte{1, 1, 1, 1}, msgs[1].Message.Value)
	require.EqualValues(t, []uint64{1, 2, 3, 4}, msgs[0].SignerIds)
	require.EqualValues(t, []uint64{5}, msgs[1].SignerIds)
}

func TestMessagesContainer_QuorumAchieved(t *testing.T) {
	c := New(3)
	c.AddMessage(&proto.SignedMessage{
		Message: &proto.Message{
			Round:  1,
			Lambda: nil,
			Value:  []byte{1, 1, 1, 1},
		},
		Signature: nil,
		SignerIds: []uint64{1, 2, 3},
	})
	res, _ := c.QuorumAchieved(1, []byte{1, 1, 1, 1})
	require.True(t, res)
	res, _ = c.QuorumAchieved(0, []byte{1, 1, 1, 1})
	require.False(t, res)
	res, _ = c.QuorumAchieved(1, []byte{1, 1, 1, 0})
	require.False(t, res)

	c.AddMessage(&proto.SignedMessage{
		Message: &proto.Message{
			Round:  2,
			Lambda: nil,
			Value:  []byte{1, 1, 1, 1},
		},
		Signature: nil,
		SignerIds: []uint64{1, 2},
	})
	res, _ = c.QuorumAchieved(2, []byte{1, 1, 1, 1})
	require.False(t, res)
	c.AddMessage(&proto.SignedMessage{
		Message: &proto.Message{
			Round:  2,
			Lambda: nil,
			Value:  []byte{1, 1, 1, 1},
		},
		Signature: nil,
		SignerIds: []uint64{3},
	})
	res, _ = c.QuorumAchieved(2, []byte{1, 1, 1, 1})
	require.True(t, res)
	res, _ = c.QuorumAchieved(3, []byte{1, 1, 1, 1})
	require.False(t, res)
	res, _ = c.QuorumAchieved(2, []byte{1, 1, 1, 0})
	require.False(t, res)
}
