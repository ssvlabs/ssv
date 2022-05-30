package msgqueue

import (
	"fmt"
	"github.com/bloxapp/ssv/protocol/v1/message"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap/zaptest"
	"strings"
	"testing"
)

func TestIndexIterator(t *testing.T) {
	logger := zaptest.NewLogger(t)

	msgs := make([]*message.SSVMessage, 0)
	for i := 1; i <= 10; i++ {
		msgs = append(msgs, &message.SSVMessage{
			MsgType: message.SSVConsensusMsgType,
			ID:      []byte(fmt.Sprintf("dummy-id-%d", i)),
			Data:    []byte(fmt.Sprintf("data-%d", i)),
		})
	}

	q, err := New(logger, WithIndexers(DefaultMsgIndexer(),
		dummyIndexer("test", "data-2"),
		dummyIndexer("test", "data-3"),
		dummyIndexer("test", "data-8")))
	require.NoError(t, err)

	for _, msg := range msgs {
		q.Add(msg)
	}

	iterator := NewIndexIterator().Add(func() string {
		return dummyIndex("test", msgs[1])
	}).Add(func() string {
		return dummyIndex("test", msgs[2])
	}).Add(func() string {
		return dummyIndex("test", msgs[6])
	})
	res := q.PopWithIterator(3, iterator)
	require.Len(t, res, 2)
}

func dummyIndex(prefix string, msg *message.SSVMessage) string {
	return fmt.Sprintf("%s/%s/id/%s", prefix, msg.GetType().String(), msg.GetIdentifier().String())
}

func dummyIndexer(prefix string, contained string) Indexer {
	return func(msg *message.SSVMessage) string {
		if msg == nil {
			return ""
		}
		if !strings.Contains(string(msg.GetData()), contained) {
			return ""
		}
		return dummyIndex(prefix, msg)
	}
}
