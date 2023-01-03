package msgqueue

import (
	"fmt"
	"testing"

	specqbft "github.com/bloxapp/ssv-spec/qbft"
	specssv "github.com/bloxapp/ssv-spec/ssv"
	spectypes "github.com/bloxapp/ssv-spec/types"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap/zaptest"
)

func TestNewMsgQueue(t *testing.T) {
	logger := zaptest.NewLogger(t)

	msg1 := &spectypes.SSVMessage{
		MsgType: spectypes.SSVConsensusMsgType,
		MsgID:   spectypes.NewMsgID([]byte("dummy-id-1"), spectypes.BNRoleAttester),
		Data:    []byte("data"),
	}
	msg2 := &spectypes.SSVMessage{
		MsgType: spectypes.SSVConsensusMsgType,
		MsgID:   spectypes.NewMsgID([]byte("dummy-id-1"), spectypes.BNRoleAttester),
		Data:    []byte("data-1"),
	}
	msg3 := &spectypes.SSVMessage{
		MsgType: spectypes.SSVConsensusMsgType,
		MsgID:   spectypes.NewMsgID([]byte("dummy-id-2"), spectypes.BNRoleAttester),
		Data:    []byte("data"),
	}

	t.Run("peek and pop", func(t *testing.T) {
		q, err := New(logger, WithIndexers(TestMsgIndexer()))

		require.NoError(t, err)
		q.Add(msg1)
		q.Add(msg2)
		q.Add(msg3)
		idx := TestMsgIndex(spectypes.SSVConsensusMsgType, spectypes.NewMsgID([]byte("dummy-id-1"), spectypes.BNRoleAttester))
		require.Equal(t, 2, q.Count(idx))
		msgs := q.Peek(2, idx)
		require.Len(t, msgs, 2)
		require.Equal(t, 2, q.Count(idx))
		msgs = q.Pop(1, idx)
		require.Len(t, msgs, 1)
		require.Equal(t, 1, q.Count(idx))
		idx2 := TestMsgIndex(spectypes.SSVConsensusMsgType, spectypes.NewMsgID([]byte("dummy-id-2"), spectypes.BNRoleAttester))
		msgs = q.Pop(5, idx2)
		require.Len(t, msgs, 1)
		require.Equal(t, 0, q.Count(idx2))
	})

	t.Run("clean", func(t *testing.T) {
		q, err := New(logger, WithIndexers(TestMsgIndexer()))
		require.NoError(t, err)
		q.Add(msg1)
		q.Add(msg2)
		q.Add(msg3)
		idx := TestMsgIndex(spectypes.SSVConsensusMsgType, spectypes.NewMsgID([]byte("dummy-id-1"), spectypes.BNRoleAttester))
		require.Equal(t, 2, q.Count(idx))
		require.Equal(t, int64(2), q.Clean(DefaultMsgCleaner(spectypes.NewMsgID([]byte("dummy-id-1"), spectypes.BNRoleAttester), spectypes.SSVConsensusMsgType)))
		require.Equal(t, 0, q.Count(idx))
	})
	t.Run("cleanSingedMsg", func(t *testing.T) {
		q, err := New(logger, WithIndexers(SignedMsgIndexer()))
		require.NoError(t, err)
		identifier := spectypes.NewMsgID([]byte("pk"), spectypes.BNRoleAttester)
		q.Add(generateConsensusMsg(t, spectypes.SSVConsensusMsgType, specqbft.Height(0), 1, identifier, specqbft.CommitMsgType))
		q.Add(generateDecidedMsg(t, spectypes.SSVConsensusMsgType, specqbft.Height(0), 1, identifier, specqbft.CommitMsgType))
		q.Add(generateConsensusMsg(t, spectypes.SSVConsensusMsgType, specqbft.Height(1), 1, identifier, specqbft.CommitMsgType))
		q.Add(generateDecidedMsg(t, spectypes.SSVConsensusMsgType, specqbft.Height(1), 1, identifier, specqbft.CommitMsgType))
		q.Add(generateConsensusMsg(t, spectypes.SSVConsensusMsgType, specqbft.Height(2), 1, identifier, specqbft.CommitMsgType))
		q.Add(generateDecidedMsg(t, spectypes.SSVConsensusMsgType, specqbft.Height(2), 1, identifier, specqbft.CommitMsgType))

		for i := 0; i <= 2; i++ {
			height := specqbft.Height(i)
			idxs := SignedMsgIndex(spectypes.SSVConsensusMsgType, identifier.String(), height, false, specqbft.CommitMsgType)
			require.Equal(t, len(idxs), 1)
			idx := idxs[0]
			require.Equal(t, 1, q.Count(idx))
			require.Equal(t, int64(2), q.Clean(SignedMsgCleaner(identifier, height)))
			require.Equal(t, 0, q.Count(idx))
		}
	})

	t.Run("cleanPostConsensusMsg", func(t *testing.T) {
		q, err := New(logger, WithIndexers(SignedPostConsensusMsgIndexer()))
		require.NoError(t, err)
		for i := 0; i <= 3; i++ {
			q.Add(generatePostConsensusMsg(t, spectypes.NewMsgID([]byte(fmt.Sprintf("pk%d", i)), spectypes.BNRoleAttester)))
		}

		for i := 0; i <= 3; i++ {
			identifier := spectypes.NewMsgID([]byte(fmt.Sprintf("pk%d", i)), spectypes.BNRoleAttester)
			idx := SignedPostConsensusMsgIndex(identifier.String())
			require.Equal(t, 1, q.Count(idx))
			require.Equal(t, int64(1), q.Clean(SignedPostConsensusMsgCleaner(identifier)))
			require.Equal(t, 0, q.Count(idx))
		}
	})
}

func generateConsensusMsg(t *testing.T, ssvMsgType spectypes.MsgType, height specqbft.Height, round specqbft.Round, id spectypes.MessageID, consensusType specqbft.MessageType) *spectypes.SSVMessage {
	ssvMsg := &spectypes.SSVMessage{
		MsgType: ssvMsgType,
		MsgID:   id,
	}

	signedMsg := specqbft.SignedMessage{
		Signature: nil,
		Signers:   nil,
		Message: &specqbft.Message{
			MsgType:    consensusType,
			Height:     height,
			Round:      round,
			Identifier: []byte{1, 2, 3, 4},
			Data:       nil,
		},
	}
	data, err := signedMsg.Encode()
	require.NoError(t, err)
	ssvMsg.Data = data
	return ssvMsg
}

func generateDecidedMsg(t *testing.T, ssvMsgType spectypes.MsgType, height specqbft.Height, round specqbft.Round, id spectypes.MessageID, consensusType specqbft.MessageType) *spectypes.SSVMessage {
	ssvMsg := &spectypes.SSVMessage{
		MsgType: ssvMsgType,
		MsgID:   id,
	}

	commitData, err := (&specqbft.CommitData{
		Data: []byte("data"),
	}).Encode()

	require.NoError(t, err)

	signedMsg := specqbft.SignedMessage{
		Signature: nil,
		Signers:   []spectypes.OperatorID{1, 2, 3},
		Message: &specqbft.Message{
			MsgType:    consensusType,
			Height:     height,
			Round:      round,
			Identifier: []byte{1, 2, 3, 4},
			Data:       commitData,
		},
	}
	data, err := signedMsg.Encode()
	require.NoError(t, err)
	ssvMsg.Data = data
	return ssvMsg
}

func generatePostConsensusMsg(t *testing.T, id spectypes.MessageID) *spectypes.SSVMessage {
	ssvMsg := &spectypes.SSVMessage{
		MsgType: spectypes.SSVPartialSignatureMsgType,
		MsgID:   id,
	}

	signedMsg := &specssv.SignedPartialSignatureMessage{
		Message: specssv.PartialSignatureMessages{
			Type: specssv.PostConsensusPartialSig,
			Messages: []*specssv.PartialSignatureMessage{
				{
					PartialSignature: make([]byte, 96),
					SigningRoot:      make([]byte, 32),
					Signer:           spectypes.OperatorID(1),
				},
			},
		},
		Signer:    spectypes.OperatorID(1),
		Signature: make([]byte, 96), // TODO should be msg sig and not decided sig
	}

	encoded, err := signedMsg.Encode()
	require.NoError(t, err)
	ssvMsg.Data = encoded
	return ssvMsg
}
