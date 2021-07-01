package ibft

import (
	"github.com/bloxapp/ssv/ibft/eventqueue"
	"github.com/bloxapp/ssv/ibft/leader"
	msgcontinmem "github.com/bloxapp/ssv/ibft/msgcont/inmem"
	"github.com/bloxapp/ssv/ibft/proto"
	"github.com/bloxapp/ssv/network"
	"github.com/bloxapp/ssv/network/msgqueue"
	"github.com/bloxapp/ssv/utils/dataval/bytesval"
	"github.com/bloxapp/ssv/validator/storage"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap/zaptest"
	"testing"
	"time"
)

func TestInstanceStop(t *testing.T) {
	secretKeys, nodes := GenerateNodes(4)
	instance := &Instance{
		MsgQueue:           msgqueue.New(),
		eventQueue:         eventqueue.New(),
		PrepareMessages:    msgcontinmem.New(3),
		PrePrepareMessages: msgcontinmem.New(3),
		Config:             proto.DefaultConsensusParams(),
		State: &proto.State{
			Round:     1,
			Stage:     proto.RoundState_PrePrepare,
			Lambda:    []byte("Lambda"),
			SeqNumber: 1,
		},
		ValidatorShare: &storage.Share{
			Committee: nodes,
			NodeID:    1,
			ShareKey:  secretKeys[1],
			PublicKey: secretKeys[1].GetPublicKey(),
		},
		ValueCheck:     bytesval.New([]byte(time.Now().Weekday().String())),
		LeaderSelector: &leader.Constant{LeaderIndex: 1},
		Logger:         zaptest.NewLogger(t),
	}
	//go instance.StartMessagePipeline()
	//go instance.StartEventLoop()
	instance.Init()

	// pre prepare
	msg := SignMsg(t, 1, secretKeys[1], &proto.Message{
		Type:      proto.RoundState_PrePrepare,
		Round:     1,
		Lambda:    []byte("Lambda"),
		Value:     []byte(time.Now().Weekday().String()),
		SeqNumber: 1,
	})
	instance.MsgQueue.AddMessage(&network.Message{
		Lambda:        msg.Message.Lambda,
		SignedMessage: msg,
		Type:          network.NetworkMsg_IBFTType,
	})

	// prepare * 2
	msg = SignMsg(t, 1, secretKeys[1], &proto.Message{
		Type:      proto.RoundState_Prepare,
		Round:     1,
		Lambda:    []byte("Lambda"),
		Value:     []byte(time.Now().Weekday().String()),
		SeqNumber: 1,
	})
	instance.MsgQueue.AddMessage(&network.Message{
		Lambda:        msg.Message.Lambda,
		SignedMessage: msg,
		Type:          network.NetworkMsg_IBFTType,
	})
	msg = SignMsg(t, 2, secretKeys[2], &proto.Message{
		Type:      proto.RoundState_Prepare,
		Round:     1,
		Lambda:    []byte("Lambda"),
		Value:     []byte(time.Now().Weekday().String()),
		SeqNumber: 1,
	})
	instance.MsgQueue.AddMessage(&network.Message{
		Lambda:        msg.Message.Lambda,
		SignedMessage: msg,
		Type:          network.NetworkMsg_IBFTType,
	})
	time.Sleep(time.Millisecond * 200)

	// stop instance and then send another msg which should not be processed
	instance.Stop()
	time.Sleep(time.Millisecond * 200)

	msg = SignMsg(t, 3, secretKeys[3], &proto.Message{
		Type:      proto.RoundState_Prepare,
		Round:     1,
		Lambda:    []byte("Lambda"),
		Value:     []byte(time.Now().Weekday().String()),
		SeqNumber: 1,
	})
	instance.MsgQueue.AddMessage(&network.Message{
		Lambda:        msg.Message.Lambda,
		SignedMessage: msg,
		Type:          network.NetworkMsg_IBFTType,
	})
	time.Sleep(time.Millisecond * 200)

	// verify
	require.Nil(t, instance.roundChangeTimer)
	require.EqualValues(t, proto.RoundState_Stopped, instance.Stage())
	require.EqualValues(t, 1, instance.MsgQueue.MsgCount(msgqueue.IBFTRoundIndexKey(instance.State.Lambda, msg.Message.SeqNumber, instance.State.Round)))
	netMsg := instance.MsgQueue.PopMessage(msgqueue.IBFTRoundIndexKey(instance.State.Lambda, msg.Message.SeqNumber, instance.State.Round))
	require.EqualValues(t, []uint64{3}, netMsg.SignedMessage.SignerIds)
}
