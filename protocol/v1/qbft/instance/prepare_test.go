package instance

import (
	"github.com/bloxapp/ssv/protocol/v1/keymanager"
	"github.com/bloxapp/ssv/utils/threadsafe"
	"github.com/stretchr/testify/require"
	"testing"

	msgcontinmem "github.com/bloxapp/ssv/ibft/instance/msgcont/inmem"
	"github.com/bloxapp/ssv/ibft/proto"
)

func TestPreparedAggregatedMsg(t *testing.T) {
	sks, nodes := GenerateNodes(4)
	instance := &Instance{
		PrepareMessages: msgcontinmem.New(3, 2),
		Config:          proto.DefaultConsensusParams(),
		ValidatorShare: &keymanager.Share{
			Committee: nodes,
			NodeID:    1,
		},
		state: &proto.State{
			Round:         threadsafe.Uint64(1),
			PreparedValue: threadsafe.Bytes(nil),
			PreparedRound: threadsafe.Uint64(0),
		},
	}

	// not prepared
	_, err := instance.PreparedAggregatedMsg()
	require.EqualError(t, err, "state not prepared")

	// set prepared state
	instance.State().PreparedRound.Set(1)
	instance.State().PreparedValue.Set([]byte("value"))

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
		PrepareMessages: msgcontinmem.New(3, 2),
		Config:          proto.DefaultConsensusParams(),
		ValidatorShare: &keymanager.Share{
			Committee: nodes,
			NodeID:    1,
			PublicKey: sks[1].GetPublicKey(),
		},
		state: &proto.State{
			Round:     threadsafe.Uint64(1),
			Lambda:    threadsafe.Bytes(nil),
			SeqNumber: threadsafe.Uint64(0),
		},
	}
	instance.fork = testingFork(instance)
	pipeline := instance.PrepareMsgPipeline()
	require.EqualValues(t, "combination of: basic msg validation, type check, lambda, sequence, authorize, add prepare msg, if first pipeline non error, continue to second, ", pipeline.Name())
}
