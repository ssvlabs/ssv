package ibft

import (
	"github.com/bloxapp/ssv/validator/storage"
	"github.com/stretchr/testify/require"
	"testing"

	msgcontinmem "github.com/bloxapp/ssv/ibft/msgcont/inmem"
	"github.com/bloxapp/ssv/ibft/proto"
)

func TestPreparedAggregatedMsg(t *testing.T) {
	sks, nodes := GenerateNodes(4)
	instance := &Instance{
		PrepareMessages: msgcontinmem.New(3),
		Config:          proto.DefaultConsensusParams(),
		ValidatorShare: &storage.Share{
			Committee: nodes,
			NodeID:    1,
			ShareKey:  sks[1],
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
	instance.PrepareMessages.AddMessage(SignMsg(t, 1, sks[1], &proto.Message{
		Type:   proto.RoundState_Prepare,
		Round:  1,
		Lambda: []byte("Lambda"),
		Value:  []byte("value"),
	}))
	instance.PrepareMessages.AddMessage(SignMsg(t, 2, sks[2], &proto.Message{
		Type:   proto.RoundState_Prepare,
		Round:  1,
		Lambda: []byte("Lambda"),
		Value:  []byte("value"),
	}))
	instance.PrepareMessages.AddMessage(SignMsg(t, 3, sks[3], &proto.Message{
		Type:   proto.RoundState_Prepare,
		Round:  1,
		Lambda: []byte("Lambda"),
		Value:  []byte("value"),
	}))

	// test aggregation
	msg, err := instance.PreparedAggregatedMsg()
	require.NoError(t, err)
	require.ElementsMatch(t, []uint64{1, 2, 3}, msg.SignerIds)

	// test that doesn't aggregate different value
	instance.PrepareMessages.AddMessage(SignMsg(t, 4, sks[4], &proto.Message{
		Type:   proto.RoundState_Prepare,
		Round:  1,
		Lambda: []byte("Lambda"),
		Value:  []byte("value2"),
	}))
	msg, err = instance.PreparedAggregatedMsg()
	require.NoError(t, err)
	require.ElementsMatch(t, []uint64{1, 2, 3}, msg.SignerIds)
}

func TestPreparePipeline(t *testing.T) {
	sks, nodes := GenerateNodes(4)
	instance := &Instance{
		PrepareMessages: msgcontinmem.New(3),
		Config:          proto.DefaultConsensusParams(),
		ValidatorShare: &storage.Share{
			Committee: nodes,
			NodeID:    1,
			PublicKey: sks[1].GetPublicKey(),
		},
		State: &proto.State{
			Round: 1,
		},
	}
	pipeline := instance.prepareMsgPipeline()
	require.EqualValues(t, "combination of: basic msg validation, type check, lambda, round, sequence, authorize, upon prepare msg, ", pipeline.Name())
}
