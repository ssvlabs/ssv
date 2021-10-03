package msgqueue

import (
	"fmt"
	"github.com/bloxapp/ssv/ibft/proto"
	"github.com/bloxapp/ssv/network"
	"github.com/stretchr/testify/require"
	"sync"
	"testing"
	"time"
)

func TestMessageQueue_PurgeAllIndexedMessages(t *testing.T) {
	msgQ := New()
	msgQ.AddMessage(newNetMsg([]byte{1, 2, 3, 4}, 1, 1, network.NetworkMsg_IBFTType))
	msgQ.AddMessage(newNetMsg([]byte{1, 2, 3, 4}, 1, 1, network.NetworkMsg_SignatureType))

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
	msgQ.AddMessage(newNetMsg([]byte{1, 2, 3, 4}, 1, 1, network.NetworkMsg_IBFTType))
	idxContent := getIndexContent(t, msgQ, "lambda_01020304_seqNumber_1_round_1")
	require.NotNil(t, idxContent)
	require.Len(t, idxContent, 1)

	msg, exist := msgQ.allMessages.Get(idxContent[0].id)
	require.True(t, exist)
	require.NotNil(t, msg)

	msgQ.AddMessage(newNetMsg([]byte{1, 2, 3, 5}, 7, 2, network.NetworkMsg_IBFTType))
	idxContent = getIndexContent(t, msgQ, "lambda_01020305_seqNumber_2_round_7")
	require.NotNil(t, idxContent)
	msg, exist = msgQ.allMessages.Get(idxContent[0].id)
	require.True(t, exist)
	require.NotNil(t, msg)

	// custom index
	msgQ.indexFuncs = append(msgQ.indexFuncs, func(msg *network.Message) []string {
		return []string{"a", "b", "c"}
	})
	msgQ.AddMessage(newNetMsg([]byte{1, 2, 3, 5}, 3, 0, network.NetworkMsg_IBFTType))

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
	msgQ.AddMessage(newNetMsg([]byte{1, 2, 3, 4}, 1, 0, network.NetworkMsg_IBFTType))

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
	msgQ.AddMessage(newNetMsg([]byte{}, 1, 0, network.NetworkMsg_IBFTType))

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

func TestMessageQueue_Concurrent(t *testing.T) {
	var wg sync.WaitGroup
	ibftMsgAdded := make(chan bool)
	sigMsgAdded := make(chan bool)
	msgQ := New()

	wg.Add(1)
	go func() {
		defer wg.Done()
		for seq := 0; seq < 10; seq++ {
			for r := 0; r < 10; r++ {
				msgQ.AddMessage(newNetMsg([]byte{1, 2, 3, 4}, uint64(r), uint64(seq), network.NetworkMsg_IBFTType))
			}
			if seq == 2 {
				ibftMsgAdded <- true
			}
			time.Sleep(1 * time.Millisecond)
		}
	}()

	wg.Add(1)
	go func() {
		defer wg.Done()
		for seq := 0; seq < 10; seq++ {
			for r := 0; r < 10; r++ {
				msgQ.AddMessage(newNetMsg([]byte{1, 2, 3, 4}, uint64(r), uint64(seq), network.NetworkMsg_SignatureType))
			}
			if seq == 2 {
				sigMsgAdded <- true
			}
			time.Sleep(1 * time.Millisecond)
		}
	}()

	// waiting for 2 messages to be added nd then starts to count and pop messages
	<- ibftMsgAdded
	<- sigMsgAdded

	wg.Add(1)
	go func() {
		defer wg.Done()
		for seq := 0; seq < 10; seq++ {
			for r := 0; r < 10; r++ {
				idx := fmt.Sprintf("lambda_01020304_seqNumber_%d_round_%d", seq, r)
				require.Equal(t, 1, msgQ.MsgCount(idx), "failed to find msg for %s", idx)
			}
			idx := fmt.Sprintf("sig_lambda_01020304_seqNumber_%d", seq)
			require.Equal(t, 10, msgQ.MsgCount(idx), "failed to find sig msg for %s", idx)
			time.Sleep(1 * time.Millisecond)
		}
	}()

	wg.Add(1)
	go func() {
		defer wg.Done()
		time.Sleep(6 * time.Millisecond)
		for seq := 0; seq < 10; seq++ {
			for r := 0; r < 10; r++ {
				require.NotNil(t, msgQ.PopMessage(fmt.Sprintf("lambda_01020304_seqNumber_%d_round_%d", seq, r)))
				require.NotNil(t, msgQ.PopMessage(fmt.Sprintf("sig_lambda_01020304_seqNumber_%d", seq)))
			}
			time.Sleep(1 * time.Millisecond)
		}
	}()

	wg.Wait()
}

func newNetMsg(lambda []byte, round, seq uint64, t network.NetworkMsg) *network.Message {
	return &network.Message{
		SignedMessage: &proto.SignedMessage{
			Message: &proto.Message{
				Lambda:    lambda,
				Round:     round,
				SeqNumber: seq,
			},
		},
		Type: t,
	}
}
