package msgqueue

import (
	"github.com/bloxapp/ssv/ibft/proto"
	"github.com/bloxapp/ssv/network"
	"github.com/stretchr/testify/require"
	"testing"
)

func TestMessageQueue_AddMessage(t *testing.T) {
	msgQ := New()
	msgQ.AddMessage(&network.Message{
		Lambda: []byte{1, 2, 3, 4},
		Msg: &proto.SignedMessage{
			Message: &proto.Message{
				Round: 1,
			},
		},
		Type: network.IBFTBroadcastingType,
	})
	require.NotNil(t, msgQ.queue["lambda_01020304_round_1"])

	msgQ.AddMessage(&network.Message{
		Lambda: []byte{1, 2, 3, 5},
		Msg: &proto.SignedMessage{
			Message: &proto.Message{
				Round: 7,
			},
		},
		Type: network.IBFTBroadcastingType,
	})
	require.NotNil(t, msgQ.queue["lambda_01020305_round_7"])

	// custom index
	msgQ.indexFuncs = append(msgQ.indexFuncs, func(msg *network.Message) []string {
		return []string{"a", "b", "c"}
	})
	msgQ.AddMessage(&network.Message{
		Lambda: []byte{1, 2, 3, 5},
		Msg: &proto.SignedMessage{
			Message: &proto.Message{
				Round: 3,
			},
		},
		Type: network.IBFTBroadcastingType,
	})

	require.NotNil(t, msgQ.queue["a"])
	require.NotNil(t, msgQ.queue["b"])
	require.NotNil(t, msgQ.queue["c"])
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
		Lambda: []byte{1, 2, 3, 4},
		Msg: &proto.SignedMessage{
			Message: &proto.Message{
				Round: 1,
			},
		},
		Type: network.IBFTBroadcastingType,
	})

	require.NotNil(t, msgQ.PopMessage("a"))
	require.Nil(t, msgQ.PopMessage("a"))
	require.Nil(t, msgQ.PopMessage("b"))
	require.Nil(t, msgQ.PopMessage("c"))
}
