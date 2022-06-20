package qbft

import (
	"github.com/bloxapp/ssv/spec/types"
	"github.com/stretchr/testify/require"
	"testing"
)

func TestMsgContainer_AddIfDoesntExist(t *testing.T) {
	t.Run("same msg and signers", func(t *testing.T) {
		c := &MsgContainer{
			Msgs: map[Round][]*SignedMessage{},
		}

		added, err := c.AddIfDoesntExist(testingSignedMsg)
		require.NoError(t, err)
		require.True(t, added)

		added, err = c.AddIfDoesntExist(testingSignedMsg)
		require.NoError(t, err)
		require.False(t, added)
	})

	t.Run("same msg different signers", func(t *testing.T) {
		c := &MsgContainer{
			Msgs: map[Round][]*SignedMessage{},
		}

		added, err := c.AddIfDoesntExist(testingSignedMsg)
		require.NoError(t, err)
		require.True(t, added)

		added, err = c.AddIfDoesntExist(SignMsg(TestingSK, 2, TestingMessage))
		require.NoError(t, err)
		require.True(t, added)
	})

	t.Run("same msg common signers", func(t *testing.T) {
		c := &MsgContainer{
			Msgs: map[Round][]*SignedMessage{},
		}

		m := testingSignedMsg.DeepCopy()
		m.Signers = []types.OperatorID{1, 2, 3, 4}
		added, err := c.AddIfDoesntExist(m)
		require.NoError(t, err)
		require.True(t, added)

		m = testingSignedMsg.DeepCopy()
		m.Signers = []types.OperatorID{1, 5, 6, 7}
		added, err = c.AddIfDoesntExist(m)
		require.NoError(t, err)
		require.True(t, added)
	})
}

func TestMsgContainer_Marshaling(t *testing.T) {
	c := &MsgContainer{
		Msgs: map[Round][]*SignedMessage{},
	}
	c.Msgs[1] = []*SignedMessage{testingSignedMsg}

	byts, err := c.Encode()
	require.NoError(t, err)

	decoded := &MsgContainer{}
	require.NoError(t, decoded.Decode(byts))

	decodedByts, err := decoded.Encode()
	require.NoError(t, err)
	require.EqualValues(t, byts, decodedByts)
}

func TestMsgContainer_UniqueSignersSetForRoundAndValue(t *testing.T) {
	t.Run("multi common signers with different values", func(t *testing.T) {
		c := &MsgContainer{
			Msgs: map[Round][]*SignedMessage{},
		}

		c.Msgs[1] = []*SignedMessage{
			{Signers: []types.OperatorID{1, 2, 3}, Message: &Message{Data: []byte{1, 2, 3, 4}}},
			{Signers: []types.OperatorID{1, 2}, Message: &Message{Data: []byte{1, 2, 3, 5}}},
			{Signers: []types.OperatorID{4}, Message: &Message{Data: []byte{1, 2, 3, 6}}},
		}
		cnt, _ := c.UniqueSignersSetForRoundAndValue(1, []byte{1, 2, 3, 4})
		require.EqualValues(t, []types.OperatorID{1, 2, 3}, cnt)

		cnt, _ = c.UniqueSignersSetForRoundAndValue(1, []byte{1, 2, 3, 6})
		require.EqualValues(t, []types.OperatorID{4}, cnt)
	})

	t.Run("multi common signers", func(t *testing.T) {
		c := &MsgContainer{
			Msgs: map[Round][]*SignedMessage{},
		}

		c.Msgs[1] = []*SignedMessage{
			{Signers: []types.OperatorID{1, 2, 3}, Message: &Message{Data: []byte{1, 2, 3, 4}}},
			{Signers: []types.OperatorID{1, 2}, Message: &Message{Data: []byte{1, 2, 3, 4}}},
			{Signers: []types.OperatorID{4}, Message: &Message{Data: []byte{1, 2, 3, 4}}},
		}
		cnt, _ := c.UniqueSignersSetForRoundAndValue(1, []byte{1, 2, 3, 4})
		require.EqualValues(t, []types.OperatorID{1, 2, 3, 4}, cnt)
	})

	t.Run("multi common signers", func(t *testing.T) {
		c := &MsgContainer{
			Msgs: map[Round][]*SignedMessage{},
		}

		c.Msgs[1] = []*SignedMessage{
			{Signers: []types.OperatorID{1, 2, 3}, Message: &Message{Data: []byte{1, 2, 3, 4}}},
			{Signers: []types.OperatorID{1, 2, 5}, Message: &Message{Data: []byte{1, 2, 3, 4}}},
			{Signers: []types.OperatorID{4}, Message: &Message{Data: []byte{1, 2, 3, 4}}},
		}
		cnt, _ := c.UniqueSignersSetForRoundAndValue(1, []byte{1, 2, 3, 4})
		require.EqualValues(t, []types.OperatorID{1, 2, 3, 4}, cnt)
	})

	t.Run("multi common signers", func(t *testing.T) {
		c := &MsgContainer{
			Msgs: map[Round][]*SignedMessage{},
		}

		c.Msgs[1] = []*SignedMessage{
			{Signers: []types.OperatorID{1, 2, 3}, Message: &Message{Data: []byte{1, 2, 3, 4}}},
			{Signers: []types.OperatorID{1, 2, 5}, Message: &Message{Data: []byte{1, 2, 3, 4}}},
			{Signers: []types.OperatorID{4}, Message: &Message{Data: []byte{1, 2, 3, 4}}},
			{Signers: []types.OperatorID{3, 7, 8, 9, 10}, Message: &Message{Data: []byte{1, 2, 3, 4}}},
		}
		cnt, _ := c.UniqueSignersSetForRoundAndValue(1, []byte{1, 2, 3, 4})
		require.EqualValues(t, []types.OperatorID{1, 2, 5, 4, 3, 7, 8, 9, 10}, cnt)
	})

	t.Run("multi common signers", func(t *testing.T) {
		c := &MsgContainer{
			Msgs: map[Round][]*SignedMessage{},
		}

		c.Msgs[1] = []*SignedMessage{
			{Signers: []types.OperatorID{1}, Message: &Message{Data: []byte{1, 2, 3, 4}}},
			{Signers: []types.OperatorID{1, 2, 3}, Message: &Message{Data: []byte{1, 2, 3, 4}}},
		}
		cnt, _ := c.UniqueSignersSetForRoundAndValue(1, []byte{1, 2, 3, 4})
		require.EqualValues(t, []types.OperatorID{1, 2, 3}, cnt)
	})

	t.Run("multi common signers", func(t *testing.T) {
		c := &MsgContainer{
			Msgs: map[Round][]*SignedMessage{},
		}

		c.Msgs[1] = []*SignedMessage{
			{Signers: []types.OperatorID{1, 2, 3}, Message: &Message{Data: []byte{1, 2, 3, 4}}},
			{Signers: []types.OperatorID{1}, Message: &Message{Data: []byte{1, 2, 3, 4}}},
		}
		cnt, _ := c.UniqueSignersSetForRoundAndValue(1, []byte{1, 2, 3, 4})
		require.EqualValues(t, []types.OperatorID{1, 2, 3}, cnt)
	})

	t.Run("no common signers", func(t *testing.T) {
		c := &MsgContainer{
			Msgs: map[Round][]*SignedMessage{},
		}

		c.Msgs[1] = []*SignedMessage{
			{Signers: []types.OperatorID{1, 2, 3}, Message: &Message{Data: []byte{1, 2, 3, 4}}},
			{Signers: []types.OperatorID{6}, Message: &Message{Data: []byte{1, 2, 3, 4}}},
			{Signers: []types.OperatorID{4, 7}, Message: &Message{Data: []byte{1, 2, 3, 4}}},
		}
		cnt, _ := c.UniqueSignersSetForRoundAndValue(1, []byte{1, 2, 3, 4})
		require.EqualValues(t, []types.OperatorID{1, 2, 3, 6, 4, 7}, cnt)
	})

	t.Run("no round", func(t *testing.T) {
		c := &MsgContainer{
			Msgs: map[Round][]*SignedMessage{},
		}

		c.Msgs[1] = []*SignedMessage{
			{Signers: []types.OperatorID{1, 2, 3}, Message: &Message{Data: []byte{1, 2, 3, 4}}},
			{Signers: []types.OperatorID{6}, Message: &Message{Data: []byte{1, 2, 3, 4}}},
			{Signers: []types.OperatorID{4, 7}, Message: &Message{Data: []byte{1, 2, 3, 4}}},
		}
		cnt, _ := c.UniqueSignersSetForRoundAndValue(2, []byte{1, 2, 3, 4})
		require.EqualValues(t, []types.OperatorID{}, cnt)
	})
}
