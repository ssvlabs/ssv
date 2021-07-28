package msgqueue

import (
	"github.com/bloxapp/ssv/ibft/proto"
	"github.com/bloxapp/ssv/network"
	"github.com/stretchr/testify/require"
	"testing"
)

func TestMessageQueue_PurgeAllIndexedMessages(t *testing.T) {
	msgQ := New()
	msgQ.AddMessage(&network.Message{
		SignedMessage: &proto.SignedMessage{
			Message: &proto.Message{
				Lambda:    []byte{1, 2, 3, 4},
				Round:     1,
				SeqNumber: 1,
			},
		},
		Type: network.NetworkMsg_IBFTType,
	})
	msgQ.AddMessage(&network.Message{
		SignedMessage: &proto.SignedMessage{
			Message: &proto.Message{
				Lambda:    []byte{1, 2, 3, 4},
				Round:     1,
				SeqNumber: 1,
			},
		},
		Type: network.NetworkMsg_SignatureType,
	})

	require.Len(t, getIndexContent(t, msgQ, "lambda_01020304_seqNumber_1_round_1"), 1)
	require.Len(t, getIndexContent(t, msgQ, "sig_lambda_01020304_seqNumber_1"), 1)

	msgQ.PurgeIndexedMessages(IBFTMessageIndexKey([]byte{1, 2, 3, 4}, 1, 1))
	require.Len(t, getIndexContent(t, msgQ, "lambda_01020304_seqNumber_1_round_1"), 0)
	require.Len(t, getIndexContent(t, msgQ, "sig_lambda_01020304_seqNumber_1"), 1)

	msgQ.PurgeIndexedMessages(SigRoundIndexKey([]byte{1, 2, 3, 4}, 1))
	require.Len(t, getIndexContent(t, msgQ, "lambda_01020304_seqNumber_1_round_1"), 0)
	require.Len(t, getIndexContent(t, msgQ, "sig_lambda_01020304_seqNumber_1"), 0)
}

func getIndexContent(t *testing.T, msgQ *MessageQueue, idx string) []messageContainer {
	raw, exist := msgQ.queue.Get(idx)
	require.True(t, exist)
	msgContainers, ok := raw.([]messageContainer)
	require.True(t, ok)
	return msgContainers
}

func TestMessageQueue_AddMessage(t *testing.T) {
	msgQ := New()
	msgQ.AddMessage(&network.Message{
		SignedMessage: &proto.SignedMessage{
			Message: &proto.Message{
				Lambda:    []byte{1, 2, 3, 4},
				Round:     1,
				SeqNumber: 1,
			},
		},
		Type: network.NetworkMsg_IBFTType,
	})
	idxContent := getIndexContent(t, msgQ, "lambda_01020304_seqNumber_1_round_1")
	require.NotNil(t, idxContent)
	require.Len(t, idxContent, 1)

	msg, exist := msgQ.allMessages.Get(idxContent[0].id)
	require.True(t, exist)
	require.NotNil(t, msg)

	msgQ.AddMessage(&network.Message{
		SignedMessage: &proto.SignedMessage{
			Message: &proto.Message{
				Lambda:    []byte{1, 2, 3, 5},
				Round:     7,
				SeqNumber: 2,
			},
		},
		Type: network.NetworkMsg_IBFTType,
	})
	idxContent = getIndexContent(t, msgQ, "lambda_01020305_seqNumber_2_round_7")
	require.NotNil(t, idxContent)
	msg, exist = msgQ.allMessages.Get(idxContent[0].id)
	require.True(t, exist)
	require.NotNil(t, msg)

	// custom index
	msgQ.indexFuncs = append(msgQ.indexFuncs, func(msg *network.Message) []string {
		return []string{"a", "b", "c"}
	})
	msgQ.AddMessage(&network.Message{
		SignedMessage: &proto.SignedMessage{
			Message: &proto.Message{
				Lambda: []byte{1, 2, 3, 5},
				Round:  3,
			},
		},
		Type: network.NetworkMsg_IBFTType,
	})

	require.NotNil(t, getIndexContent(t, msgQ, "a"))
	require.NotNil(t, getIndexContent(t, msgQ, "b"))
	require.NotNil(t, getIndexContent(t, msgQ, "c"))
	require.Nil(t, msgQ.PopMessage("d"))
}

func TestMessageQueue_PopMessage(t *testing.T) {
	msgQ := New()
	msgQ.indexFuncs = []IndexFunc{
		func(msg *network.Message) []string {
			return []string{"a", "b", "c"}
		},
	}
	msgQ.AddMessage(&network.Message{
		SignedMessage: &proto.SignedMessage{
			Message: &proto.Message{
				Lambda: []byte{1, 2, 3, 4},
				Round:  1,
			},
		},
		Type: network.NetworkMsg_IBFTType,
	})

	raw, exist := msgQ.allMessages.Get(getIndexContent(t, msgQ, "a")[0].id)
	require.True(t, exist)
	msgID, ok := raw.(messageContainer)
	require.True(t, ok)
	require.NotNil(t, msgQ.PopMessage("a"))
	require.Nil(t, msgQ.PopMessage("a"))
	msg, exist := msgQ.allMessages.Get(msgID.id)
	require.False(t, exist)
	require.Nil(t, msg)
	require.Nil(t, msgQ.PopMessage("b"))
	require.Nil(t, msgQ.PopMessage("c"))
}

func TestMessageQueue_DeleteMessagesWithIds(t *testing.T) {
	msgQ := New()
	msgQ.indexFuncs = []IndexFunc{
		func(msg *network.Message) []string {
			return []string{"a", "b", "c"}
		},
	}
	msgQ.AddMessage(&network.Message{
		SignedMessage: &proto.SignedMessage{
			Message: &proto.Message{
				Round: 1,
			},
		},
		Type: network.NetworkMsg_IBFTType,
	})

	raw, exist := msgQ.allMessages.Get(getIndexContent(t, msgQ, "a")[0].id)
	require.True(t, exist)
	msgID, ok := raw.(messageContainer)
	require.True(t, ok)
	msgQ.DeleteMessagesWithIds([]string{msgID.id})
	require.Nil(t, msgQ.PopMessage("a"))
	require.Nil(t, msgQ.PopMessage("b"))
	require.Nil(t, msgQ.PopMessage("c"))
	msg, exist := msgQ.allMessages.Get(msgID.id)
	require.False(t, exist)
	require.Nil(t, msg)
}
