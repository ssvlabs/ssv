package msgqueue

import (
	"testing"

	spec "github.com/attestantio/go-eth2-client/spec/phase0"
	specqbft "github.com/bloxapp/ssv-spec/qbft"
	"github.com/bloxapp/ssv-spec/ssv"
	spectypes "github.com/bloxapp/ssv-spec/types"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap/zaptest"

	"github.com/bloxapp/ssv/protocol/v1/message"
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
		identifier := message.NewIdentifier([]byte("pk"), spectypes.BNRoleAttester)
		q.Add(generateConsensusMsg(t, message.SSVConsensusMsgType, specqbft.Height(0), 1, identifier, specqbft.CommitMsgType))
		q.Add(generateConsensusMsg(t, message.SSVDecidedMsgType, specqbft.Height(0), 1, identifier, specqbft.CommitMsgType))
		q.Add(generateConsensusMsg(t, message.SSVConsensusMsgType, specqbft.Height(1), 1, identifier, specqbft.CommitMsgType))
		q.Add(generateConsensusMsg(t, message.SSVDecidedMsgType, specqbft.Height(1), 1, identifier, specqbft.CommitMsgType))
		q.Add(generateConsensusMsg(t, message.SSVConsensusMsgType, specqbft.Height(2), 1, identifier, specqbft.CommitMsgType))
		q.Add(generateConsensusMsg(t, message.SSVDecidedMsgType, specqbft.Height(2), 1, identifier, specqbft.CommitMsgType))

		for i := 0; i <= 2; i++ {
			height := specqbft.Height(i)
			idxs := SignedMsgIndex(message.SSVConsensusMsgType, identifier.String(), height, specqbft.CommitMsgType)
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
		identifier := message.NewIdentifier([]byte("pk"), spectypes.BNRoleAttester)
		q.Add(generatePostConsensusMsg(t, 0, identifier))
		q.Add(generatePostConsensusMsg(t, 1, identifier))
		q.Add(generatePostConsensusMsg(t, 2, identifier))
		q.Add(generatePostConsensusMsg(t, 3, identifier))

		for i := 0; i <= 3; i++ {
			slot := spec.Slot(i)
			idx := SignedPostConsensusMsgIndex(identifier.String(), slot)
			require.Equal(t, 1, q.Count(idx))
			require.Equal(t, int64(1), q.Clean(SignedPostConsensusMsgCleaner(identifier, slot)))
			require.Equal(t, 0, q.Count(idx))
		}
	})
}

func generateConsensusMsg(t *testing.T, ssvMsgType message.MsgType, height specqbft.Height, round specqbft.Round, id message.Identifier, consensusType specqbft.MessageType) *message.SSVMessage {
	ssvMsg := &message.SSVMessage{
		MsgType: ssvMsgType,
		ID:      id,
	}

	signedMsg := specqbft.SignedMessage{
		Signature: nil,
		Signers:   nil,
		Message: &specqbft.Message{
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

func generatePostConsensusMsg(t *testing.T, slot spec.Slot, id message.Identifier) *message.SSVMessage {
	ssvMsg := &message.SSVMessage{
		MsgType: message.SSVPostConsensusMsgType,
		ID:      id,
	}

	signedMsg := &ssv.SignedPartialSignatureMessage{
		Type: ssv.PostConsensusPartialSig,
		Messages: ssv.PartialSignatureMessages{
			&ssv.PartialSignatureMessage{
				Slot:             slot,
				PartialSignature: make([]byte, 96),
				SigningRoot:      make([]byte, 32),
				Signers:          []spectypes.OperatorID{1},
			},
		},
		Signature: make([]byte, 96), // TODO should be msg sig and not decided sig
		Signers:   []spectypes.OperatorID{1},
	}

	encoded, err := signedMsg.Encode()
	require.NoError(t, err)
	ssvMsg.Data = encoded
	return ssvMsg
}
