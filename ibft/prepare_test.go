package ibft

import (
	"encoding/hex"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	msgcontinmem "github.com/bloxapp/ssv/ibft/msgcont/inmem"
	"github.com/bloxapp/ssv/ibft/proto"
	ibfttesting "github.com/bloxapp/ssv/ibft/spectesting"
)

func TestPrepareQuorum(t *testing.T) {
	secretKeys, nodes := ibfttesting.GenerateNodes(4)
	instance := &Instance{
		PrepareMessages: msgcontinmem.New(),
		Params: &proto.InstanceParams{
			ConsensusParams: proto.DefaultConsensusParams(),
			IbftCommittee:   nodes,
		},
	}

	msg := ibfttesting.SignMsg(0, secretKeys[0], &proto.Message{
		Type:   proto.RoundState_Prepare,
		Round:  2,
		Lambda: []byte("Lambda"),
		Value:  []byte(time.Now().Weekday().String()),
	})
	instance.PrepareMessages.AddMessage(msg)

	msg = ibfttesting.SignMsg(1, secretKeys[1], &proto.Message{
		Type:   proto.RoundState_Prepare,
		Round:  2,
		Lambda: []byte("Lambda"),
		Value:  []byte(time.Now().Weekday().String()),
	})
	instance.PrepareMessages.AddMessage(msg)

	// no quorum yet
	res, totalSignedMsgs, committeeSize := instance.prepareQuorum(2, []byte(time.Now().Weekday().String()))
	require.False(t, res)
	require.EqualValues(t, totalSignedMsgs, 2)
	require.EqualValues(t, committeeSize, 4)

	// test adding different value
	msg = ibfttesting.SignMsg(2, secretKeys[2], &proto.Message{
		Type:   proto.RoundState_Prepare,
		Round:  2,
		Lambda: []byte("Lambda"),
		Value:  []byte("wrong"),
	})
	instance.PrepareMessages.AddMessage(msg)

	res, totalSignedMsgs, committeeSize = instance.prepareQuorum(2, []byte(time.Now().Weekday().String()))
	require.False(t, res)
	require.EqualValues(t, totalSignedMsgs, 2)
	require.EqualValues(t, committeeSize, 4)

	// test adding different round
	msg = ibfttesting.SignMsg(2, secretKeys[2], &proto.Message{
		Type:   proto.RoundState_Prepare,
		Round:  3,
		Lambda: []byte("Lambda"),
		Value:  []byte(time.Now().Weekday().String()),
	})
	instance.PrepareMessages.AddMessage(msg)

	res, totalSignedMsgs, committeeSize = instance.prepareQuorum(2, []byte(time.Now().Weekday().String()))
	require.False(t, res)
	require.EqualValues(t, totalSignedMsgs, 2)
	require.EqualValues(t, committeeSize, 4)

	// test valid quorum
	msg = ibfttesting.SignMsg(2, secretKeys[2], &proto.Message{
		Type:   proto.RoundState_Prepare,
		Round:  2,
		Lambda: []byte("Lambda"),
		Value:  []byte(time.Now().Weekday().String()),
	})
	instance.PrepareMessages.AddMessage(msg)

	res, totalSignedMsgs, committeeSize = instance.prepareQuorum(2, []byte(time.Now().Weekday().String()))
	require.True(t, res)
	require.EqualValues(t, totalSignedMsgs, 3)
	require.EqualValues(t, committeeSize, 4)
}

func TestBatchedPrepareMsgsAndQuorum(t *testing.T) {
	_, nodes := ibfttesting.GenerateNodes(4)
	instance := &Instance{
		PrepareMessages: msgcontinmem.New(),
		Params: &proto.InstanceParams{
			ConsensusParams: proto.DefaultConsensusParams(),
			IbftCommittee:   nodes,
		},
		State: &proto.State{
			Round: 1,
		},
	}

	instance.PrepareMessages.AddMessage(&proto.SignedMessage{
		Message: &proto.Message{
			Type:   proto.RoundState_Prepare,
			Round:  1,
			Lambda: []byte("Lambda"),
			Value:  []byte("value"),
		},
		SignerIds: []uint64{1},
	})
	instance.PrepareMessages.AddMessage(&proto.SignedMessage{
		Message: &proto.Message{
			Type:   proto.RoundState_Prepare,
			Round:  1,
			Lambda: []byte("Lambda"),
			Value:  []byte("value"),
		},
		SignerIds: []uint64{2},
	})
	instance.PrepareMessages.AddMessage(&proto.SignedMessage{
		Message: &proto.Message{
			Type:   proto.RoundState_Prepare,
			Round:  1,
			Lambda: []byte("Lambda"),
			Value:  []byte("value"),
		},
		SignerIds: []uint64{3},
	})
	instance.PrepareMessages.AddMessage(&proto.SignedMessage{
		Message: &proto.Message{
			Type:   proto.RoundState_Prepare,
			Round:  1,
			Lambda: []byte("Lambda"),
			Value:  []byte("value2"),
		},
		SignerIds: []uint64{4},
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

func TestPreparedAggregatedMsg(t *testing.T) {
	sks, nodes := ibfttesting.GenerateNodes(4)
	instance := &Instance{
		PrepareMessages: msgcontinmem.New(),
		Params: &proto.InstanceParams{
			ConsensusParams: proto.DefaultConsensusParams(),
			IbftCommittee:   nodes,
		},
		State: &proto.State{
			Round: 1,
		},
	}

	// not prepared
	_, err := instance.PreparedAggregatedMsg()
	require.EqualError(t, err, "state not prepared")

	// set prepared state
	instance.State.PreparedRound = 1
	instance.State.PreparedValue = []byte("value")

	// test prepared but no msgs
	_, err = instance.PreparedAggregatedMsg()
	require.EqualError(t, err, "no prepare msgs")

	// test valid aggregation
	instance.PrepareMessages.AddMessage(ibfttesting.SignMsg(0, sks[0], &proto.Message{
		Type:   proto.RoundState_Prepare,
		Round:  1,
		Lambda: []byte("Lambda"),
		Value:  []byte("value"),
	}))
	instance.PrepareMessages.AddMessage(ibfttesting.SignMsg(1, sks[1], &proto.Message{
		Type:   proto.RoundState_Prepare,
		Round:  1,
		Lambda: []byte("Lambda"),
		Value:  []byte("value"),
	}))
	instance.PrepareMessages.AddMessage(ibfttesting.SignMsg(2, sks[2], &proto.Message{
		Type:   proto.RoundState_Prepare,
		Round:  1,
		Lambda: []byte("Lambda"),
		Value:  []byte("value"),
	}))

	// test aggregation
	msg, err := instance.PreparedAggregatedMsg()
	require.NoError(t, err)
	require.ElementsMatch(t, []uint64{0, 1, 2}, msg.SignerIds)

	// test that doesn't aggregate different value
	instance.PrepareMessages.AddMessage(ibfttesting.SignMsg(3, sks[3], &proto.Message{
		Type:   proto.RoundState_Prepare,
		Round:  1,
		Lambda: []byte("Lambda"),
		Value:  []byte("value2"),
	}))
	msg, err = instance.PreparedAggregatedMsg()
	require.NoError(t, err)
	require.ElementsMatch(t, []uint64{0, 1, 2}, msg.SignerIds)
}
