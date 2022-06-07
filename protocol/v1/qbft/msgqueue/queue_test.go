package msgqueue

import (
	"github.com/bloxapp/ssv/protocol/v1/message"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap/zaptest"
	"testing"
)

func TestNewMsgQueue(t *testing.T) {
	logger := zaptest.NewLogger(t)

	msg1 := &message.SSVMessage{
		MsgType: message.SSVConsensusMsgType,
		ID:      []byte("dummy-id-1"),
		Data:    []byte("data"),
	}
	msg2 := &message.SSVMessage{
		MsgType: message.SSVConsensusMsgType,
		ID:      []byte("dummy-id-1"),
		Data:    []byte("data-1"),
	}
	msg3 := &message.SSVMessage{
		MsgType: message.SSVConsensusMsgType,
		ID:      []byte("dummy-id-2"),
		Data:    []byte("data"),
	}

	t.Run("peek and pop", func(t *testing.T) {
		q, err := New(logger, WithIndexers(DefaultMsgIndexer()))

		require.NoError(t, err)
		q.Add(msg1)
		q.Add(msg2)
		q.Add(msg3)
		idx := DefaultMsgIndex(message.SSVConsensusMsgType, []byte("dummy-id-1"))
		require.Equal(t, 2, q.Count(idx))
		msgs := q.Peek(2, idx)
		require.Len(t, msgs, 2)
		require.Equal(t, 2, q.Count(idx))
		msgs = q.Pop(1, idx)
		require.Len(t, msgs, 1)
		require.Equal(t, 1, q.Count(idx))
		idx2 := DefaultMsgIndex(message.SSVConsensusMsgType, []byte("dummy-id-2"))
		msgs = q.Pop(5, idx2)
		require.Len(t, msgs, 1)
		require.Equal(t, 0, q.Count(idx2))
	})

	t.Run("clean", func(t *testing.T) {
		q, err := New(logger, WithIndexers(DefaultMsgIndexer()))
		require.NoError(t, err)
		q.Add(msg1)
		q.Add(msg2)
		q.Add(msg3)
		idx := DefaultMsgIndex(message.SSVConsensusMsgType, []byte("dummy-id-1"))
		require.Equal(t, 2, q.Count(idx))
		require.Equal(t, int64(2), q.Clean(DefaultMsgCleaner([]byte("dummy-id-1"), message.SSVConsensusMsgType)))
		require.Equal(t, 0, q.Count(idx))
	})
	t.Run("cleanSingedMsg", func(t *testing.T) {
		q, err := New(logger, WithIndexers(SignedMsgIndexer()))
		require.NoError(t, err)
		identifier := message.NewIdentifier([]byte("pk"), message.RoleTypeAttester)
		q.Add(generateConsensusMsg(t, message.SSVConsensusMsgType, message.Height(0), 1, identifier, message.CommitMsgType))
		q.Add(generateConsensusMsg(t, message.SSVDecidedMsgType, message.Height(0), 1, identifier, message.CommitMsgType))
		q.Add(generateConsensusMsg(t, message.SSVConsensusMsgType, message.Height(1), 1, identifier, message.CommitMsgType))
		q.Add(generateConsensusMsg(t, message.SSVDecidedMsgType, message.Height(1), 1, identifier, message.CommitMsgType))
		q.Add(generateConsensusMsg(t, message.SSVConsensusMsgType, message.Height(2), 1, identifier, message.CommitMsgType))
		q.Add(generateConsensusMsg(t, message.SSVDecidedMsgType, message.Height(2), 1, identifier, message.CommitMsgType))

		for i := 0; i <= 2; i++ {
			height := message.Height(i)
			idxs := SignedMsgIndex(message.SSVConsensusMsgType, identifier.String(), height, message.CommitMsgType)
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
		identifier := message.NewIdentifier([]byte("pk"), message.RoleTypeAttester)
		q.Add(generatePostConsensusMsg(t, 0, identifier))
		q.Add(generatePostConsensusMsg(t, 1, identifier))
		q.Add(generatePostConsensusMsg(t, 2, identifier))
		q.Add(generatePostConsensusMsg(t, 3, identifier))

		for i := 0; i <= 3; i++ {
			height := message.Height(i)
			idx := SignedPostConsensusMsgIndex(identifier.String(), height)
			require.Equal(t, 1, q.Count(idx))
			require.Equal(t, int64(1), q.Clean(SignedPostConsensusMsgCleaner(identifier, height)))
			require.Equal(t, 0, q.Count(idx))
		}
	})
}

func generateConsensusMsg(t *testing.T, ssvMsgType message.MsgType, height message.Height, round message.Round, id message.Identifier, consensusType message.ConsensusMessageType) *message.SSVMessage {
	ssvMsg := &message.SSVMessage{
		MsgType: ssvMsgType,
		ID:      id,
	}

	signedMsg := message.SignedMessage{
		Signature: nil,
		Signers:   nil,
		Message: &message.ConsensusMessage{
			MsgType:    consensusType,
			Height:     height,
			Round:      round,
			Identifier: nil,
			Data:       nil,
		},
	}
	data, err := signedMsg.Encode()
	require.NoError(t, err)
	ssvMsg.Data = data
	return ssvMsg
}

func generatePostConsensusMsg(t *testing.T, height message.Height, id message.Identifier) *message.SSVMessage {
	ssvMsg := &message.SSVMessage{
		MsgType: message.SSVPostConsensusMsgType,
		ID:      id,
	}

	pcm := &message.PostConsensusMessage{
		Height:          height,
		DutySignature:   []byte("sig"),
		DutySigningRoot: []byte("root"),
		Signers:         []message.OperatorID{1, 2, 3},
	}

	spcm := message.SignedPostConsensusMessage{
		Message:   pcm,
		Signature: []byte("sig1"),
		Signers:   []message.OperatorID{1, 2, 3},
	}

	encoded, err := spcm.Encode()
	require.NoError(t, err)
	ssvMsg.Data = encoded
	return ssvMsg
}
