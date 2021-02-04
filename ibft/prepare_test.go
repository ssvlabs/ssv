package ibft

import (
	"encoding/hex"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/bloxapp/ssv/ibft/msgcont"
	"github.com/bloxapp/ssv/ibft/proto"
	"github.com/bloxapp/ssv/ibft/val/weekday"
)

func TestPrepareQuorum(t *testing.T) {
	secretKeys, nodes := generateNodes(4)
	instance := &Instance{
		prepareMessages: msgcont.NewMessagesContainer(),
		params: &proto.InstanceParams{
			ConsensusParams: proto.DefaultConsensusParams(),
			IbftCommittee:   nodes,
		},
	}

	msg := signMsg(0, secretKeys[0], &proto.Message{
		Type:   proto.RoundState_Prepare,
		Round:  2,
		Lambda: []byte("lambda"),
		Value:  []byte(time.Now().Weekday().String()),
	})
	instance.prepareMessages.AddMessage(msg)

	msg = signMsg(1, secretKeys[1], &proto.Message{
		Type:   proto.RoundState_Prepare,
		Round:  2,
		Lambda: []byte("lambda"),
		Value:  []byte(time.Now().Weekday().String()),
	})
	instance.prepareMessages.AddMessage(msg)

	// no quorum yet
	res, totalSignedMsgs, committeeSize := instance.prepareQuorum(2, []byte(time.Now().Weekday().String()))
	require.False(t, res)
	require.EqualValues(t, totalSignedMsgs, 2)
	require.EqualValues(t, committeeSize, 4)

	// test adding different value
	msg = signMsg(2, secretKeys[2], &proto.Message{
		Type:   proto.RoundState_Prepare,
		Round:  2,
		Lambda: []byte("lambda"),
		Value:  []byte("wrong"),
	})
	instance.prepareMessages.AddMessage(msg)

	res, totalSignedMsgs, committeeSize = instance.prepareQuorum(2, []byte(time.Now().Weekday().String()))
	require.False(t, res)
	require.EqualValues(t, totalSignedMsgs, 2)
	require.EqualValues(t, committeeSize, 4)

	// test adding different round
	msg = signMsg(2, secretKeys[2], &proto.Message{
		Type:   proto.RoundState_Prepare,
		Round:  3,
		Lambda: []byte("lambda"),
		Value:  []byte(time.Now().Weekday().String()),
	})
	instance.prepareMessages.AddMessage(msg)

	res, totalSignedMsgs, committeeSize = instance.prepareQuorum(2, []byte(time.Now().Weekday().String()))
	require.False(t, res)
	require.EqualValues(t, totalSignedMsgs, 2)
	require.EqualValues(t, committeeSize, 4)

	// test valid quorum
	msg = signMsg(2, secretKeys[2], &proto.Message{
		Type:   proto.RoundState_Prepare,
		Round:  2,
		Lambda: []byte("lambda"),
		Value:  []byte(time.Now().Weekday().String()),
	})
	instance.prepareMessages.AddMessage(msg)

	res, totalSignedMsgs, committeeSize = instance.prepareQuorum(2, []byte(time.Now().Weekday().String()))
	require.True(t, res)
	require.EqualValues(t, totalSignedMsgs, 3)
	require.EqualValues(t, committeeSize, 4)
}

func TestValidatePrepareMsg(t *testing.T) {
	secretKeys, nodes := generateNodes(4)
	instance := &Instance{
		prepareMessages:    msgcont.NewMessagesContainer(),
		prePrepareMessages: msgcont.NewMessagesContainer(),
		params: &proto.InstanceParams{
			ConsensusParams: proto.DefaultConsensusParams(),
			IbftCommittee:   nodes,
		},
		State: &proto.State{
			Round:  1,
			Lambda: []byte("lambda"),
		},
		consensus: weekday.New(),
	}

	// test no valid pre-prepare msg
	msg := signMsg(1, secretKeys[1], &proto.Message{
		Type:   proto.RoundState_Prepare,
		Round:  2,
		Lambda: []byte("lambda"),
		Value:  []byte(time.Now().Weekday().String()),
	})
	require.EqualError(t, instance.validatePrepareMsg()(msg), "no pre-prepare value found")

	// test invalid prepare value
	msg = signMsg(2, secretKeys[2], &proto.Message{
		Type:   proto.RoundState_PrePrepare,
		Round:  2,
		Lambda: []byte("lambda"),
		Value:  []byte(time.Now().Weekday().String()),
	})
	instance.prePrepareMessages.AddMessage(msg)

	msg = signMsg(1, secretKeys[1], &proto.Message{
		Type:   proto.RoundState_Prepare,
		Round:  2,
		Lambda: []byte("lambda"),
		Value:  []byte("invalid"),
	})
	require.EqualError(t, instance.validatePrepareMsg()(msg), "pre-prepare value not equal to prepare message value")

	// test valid prepare value
	msg = signMsg(1, secretKeys[1], &proto.Message{
		Type:   proto.RoundState_Prepare,
		Round:  2,
		Lambda: []byte("lambda"),
		Value:  []byte(time.Now().Weekday().String()),
	})
	require.NoError(t, instance.validatePrepareMsg()(msg))
}

func TestBatchedPrepareMsgsAndQuorum(t *testing.T) {
	_, nodes := generateNodes(4)
	instance := &Instance{
		prepareMessages: msgcont.NewMessagesContainer(),
		params: &proto.InstanceParams{
			ConsensusParams: proto.DefaultConsensusParams(),
			IbftCommittee:   nodes,
		},
		State: &proto.State{
			Round: 1,
		},
	}

	instance.prepareMessages.AddMessage(&proto.SignedMessage{
		Message: &proto.Message{
			Type:   proto.RoundState_Prepare,
			Round:  1,
			Lambda: []byte("lambda"),
			Value:  []byte("value"),
		},
		IbftId: 1,
	})
	instance.prepareMessages.AddMessage(&proto.SignedMessage{
		Message: &proto.Message{
			Type:   proto.RoundState_Prepare,
			Round:  1,
			Lambda: []byte("lambda"),
			Value:  []byte("value"),
		},
		IbftId: 2,
	})
	instance.prepareMessages.AddMessage(&proto.SignedMessage{
		Message: &proto.Message{
			Type:   proto.RoundState_Prepare,
			Round:  1,
			Lambda: []byte("lambda"),
			Value:  []byte("value"),
		},
		IbftId: 3,
	})
	instance.prepareMessages.AddMessage(&proto.SignedMessage{
		Message: &proto.Message{
			Type:   proto.RoundState_Prepare,
			Round:  1,
			Lambda: []byte("lambda"),
			Value:  []byte("value2"),
		},
		IbftId: 4,
	})

	// test batch
	res := instance.batchedPrepareMsgs(1)
	require.Len(t, res, 2)
	require.Len(t, res[hex.EncodeToString([]byte("value"))], 3)
	require.Len(t, res[hex.EncodeToString([]byte("value2"))], 1)

	// test valid quorum
	quorum, tt, n := instance.prepareQuorum(1, []byte("value"))
	require.True(t, quorum)
	require.EqualValues(t, 3, tt)
	require.EqualValues(t, 4, n)

	// test invalid quorum
	quorum, tt, n = instance.prepareQuorum(1, []byte("value2"))
	require.False(t, quorum)
	require.EqualValues(t, 1, tt)
	require.EqualValues(t, 4, n)
}
