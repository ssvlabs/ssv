package msgqueue

import (
	"github.com/bloxapp/ssv/ibft/proto"
	"github.com/stretchr/testify/require"
	"testing"
	"time"
)

func TestMessageQueue(t *testing.T) {
	msgQ := New()

	receivedFirstMsg, receivedSecondMsg := false, false
	msgQ.Subscribe(func(message *proto.SignedMessage) {
		if message.Message.Round == 1 {
			receivedFirstMsg = true
		}
		if message.Message.Round == 2 {
			receivedSecondMsg = true
		}
	})
	msgQ.SetRound(1)

	// send current and future round message
	msgQ.AddMessage(&proto.SignedMessage{
		Message: &proto.Message{
			Round: 1,
		},
	})
	msgQ.AddMessage(&proto.SignedMessage{
		Message: &proto.Message{
			Round: 2,
		},
	})
	time.Sleep(time.Millisecond * 200)
	require.True(t, receivedFirstMsg)
	require.False(t, receivedSecondMsg)

	// move stage
	msgQ.SetRound(2)
	time.Sleep(time.Millisecond * 200)
	require.True(t, receivedSecondMsg)
}
