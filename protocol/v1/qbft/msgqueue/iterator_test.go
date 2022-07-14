package msgqueue

import (
	"fmt"
	spectypes "github.com/bloxapp/ssv-spec/types"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"go.uber.org/zap/zaptest"
)

func TestIndexIterator(t *testing.T) {
	logger := zaptest.NewLogger(t)

	msgs := make([]*spectypes.SSVMessage, 0)
	for i := 1; i <= 10; i++ {
		msgs = append(msgs, &spectypes.SSVMessage{
			MsgType: spectypes.SSVConsensusMsgType,
			MsgID:   spectypes.NewMsgID([]byte(fmt.Sprintf("dummy-id-%d", i)), spectypes.BNRoleAttester),
			Data:    []byte(fmt.Sprintf("data-%d", i)),
		})
	}

	q, err := New(logger, WithIndexers(DefaultMsgIndexer(),
		dummyIndexer("data-2"),
		dummyIndexer("data-3"),
		dummyIndexer("data-8")))
	require.NoError(t, err)

	for _, msg := range msgs {
		q.Add(msg)
	}

	iterator := NewIndexIterator().Add(func() Index {
		return dummyIndex(msgs[1])
	}).Add(func() Index {
		return dummyIndex(msgs[2])
	}).Add(func() Index {
		return dummyIndex(msgs[6])
	})
	res := q.PopIndices(3, iterator)
	require.Len(t, res, 2)
}

func dummyIndex(msg *spectypes.SSVMessage) Index {
	return Index{
		Mt:  msg.GetType(),
		ID:  msg.GetID().String(),
		H:   -1,
		Cmt: -1,
	}
}

func dummyIndexer(contained string) Indexer {
	return func(msg *spectypes.SSVMessage) Index {
		if msg == nil {
			return Index{}
		}
		if !strings.Contains(string(msg.GetData()), contained) {
			return Index{}
		}
		return dummyIndex(msg)
	}
}
