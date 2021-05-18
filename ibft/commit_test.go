package ibft

import (
	"testing"

	"github.com/stretchr/testify/require"

	msgcontinmem "github.com/bloxapp/ssv/ibft/msgcont/inmem"
	"github.com/bloxapp/ssv/ibft/proto"
)

func TestCommittedAggregatedMsg(t *testing.T) {
	sks, nodes := GenerateNodes(4)
	instance := &Instance{
		CommitMessages: msgcontinmem.New(3),
		Params: &proto.InstanceParams{
			ConsensusParams: proto.DefaultConsensusParams(),
			IbftCommittee:   nodes,
		},
		State: &proto.State{
			Round: 3,
		},
	}

	// not prepared
	_, err := instance.CommittedAggregatedMsg()
	require.EqualError(t, err, "state not prepared")

	// set prepared state
	instance.State.PreparedRound = 1
	instance.State.PreparedValue = []byte("value")

	// test prepared but no committed msgs
	_, err = instance.CommittedAggregatedMsg()
	require.EqualError(t, err, "no commit msgs")

	// test valid aggregation
	instance.CommitMessages.AddMessage(SignMsg(t, 1, sks[1], &proto.Message{
		Type:   proto.RoundState_Commit,
		Round:  3,
		Lambda: []byte("Lambda"),
		Value:  []byte("value"),
	}))
	instance.CommitMessages.AddMessage(SignMsg(t, 2, sks[2], &proto.Message{
		Type:   proto.RoundState_Commit,
		Round:  3,
		Lambda: []byte("Lambda"),
		Value:  []byte("value"),
	}))
	instance.CommitMessages.AddMessage(SignMsg(t, 3, sks[3], &proto.Message{
		Type:   proto.RoundState_Commit,
		Round:  3,
		Lambda: []byte("Lambda"),
		Value:  []byte("value"),
	}))

	// test aggregation
	msg, err := instance.CommittedAggregatedMsg()
	require.NoError(t, err)
	require.ElementsMatch(t, []uint64{1, 2, 3}, msg.SignerIds)

	// test that doesn't aggregate different value
	instance.CommitMessages.AddMessage(SignMsg(t, 3, sks[3], &proto.Message{
		Type:   proto.RoundState_Commit,
		Round:  3,
		Lambda: []byte("Lambda"),
		Value:  []byte("value2"),
	}))
	msg, err = instance.CommittedAggregatedMsg()
	require.NoError(t, err)
	require.ElementsMatch(t, []uint64{1, 2, 3}, msg.SignerIds)

	// test verification
	params := &proto.InstanceParams{
		ConsensusParams: proto.DefaultConsensusParams(),
		IbftCommittee:   nodes,
	}
	require.NoError(t, params.VerifySignedMessage(msg))
}

func TestCommitPipeline(t *testing.T) {
	_, nodes := GenerateNodes(4)
	instance := &Instance{
		PrepareMessages: msgcontinmem.New(3),
		Params: &proto.InstanceParams{
			ConsensusParams: proto.DefaultConsensusParams(),
			IbftCommittee:   nodes,
		},
		State: &proto.State{
			Round: 1,
		},
	}
	pipeline := instance.commitMsgPipeline()
	require.EqualValues(t, "combination of: type check, lambda, round, validator PK, sequence, authorize, upon commit msg, ", pipeline.Name())
}
