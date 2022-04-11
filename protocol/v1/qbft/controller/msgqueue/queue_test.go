package msgqueue

import (
	"github.com/bloxapp/ssv/protocol/v1/message"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap/zaptest"
	"testing"
)

func TestNewMsgQueue(t *testing.T) {
	logger := zaptest.NewLogger(t)
	q, err := New(logger, WithIndexers(DefaultMsgIndexer()))
	require.NoError(t, err)

	q.Add(&message.SSVMessage{
		MsgType: message.SSVConsensusMsgType,
		ID:      []byte("dummy-id-1"),
		Data:    []byte("data"),
	})
	q.Add(&message.SSVMessage{
		MsgType: message.SSVConsensusMsgType,
		ID:      []byte("dummy-id-1"),
		Data:    []byte("data-1"),
	})
	q.Add(&message.SSVMessage{
		MsgType: message.SSVConsensusMsgType,
		ID:      []byte("dummy-id-2"),
		Data:    []byte("data"),
	})

	idx := DefaultMsgIndex(message.SSVConsensusMsgType, []byte("dummy-id-1"))
	require.Equal(t, 2, q.Count(idx))
	msgs := q.Peek(idx, 2)
	require.Len(t, msgs, 2)
	msgs = q.Pop(idx, 1)
	require.Len(t, msgs, 1)
	require.Equal(t, 1, q.Count(idx))
	idx2 := DefaultMsgIndex(message.SSVConsensusMsgType, []byte("dummy-id-2"))
	msgs = q.Pop(idx2, 5)
	require.Len(t, msgs, 1)
	require.Equal(t, 0, q.Count(idx2))
}
