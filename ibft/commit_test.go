package ibft

import (
	"testing"

	"github.com/stretchr/testify/require"

	msgcontinmem "github.com/bloxapp/ssv/ibft/msgcont/inmem"
	"github.com/bloxapp/ssv/ibft/proto"
	ibfttesting "github.com/bloxapp/ssv/ibft/testing"
)

func TestCommittedAggregatedMsg(t *testing.T) {
	sks, nodes := ibfttesting.GenerateNodes(4)
	instance := &Instance{
		commitMessages: msgcontinmem.New(),
		params: &proto.InstanceParams{
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
	instance.commitMessages.AddMessage(ibfttesting.SignMsg(0, sks[0], &proto.Message{
		Type:   proto.RoundState_Commit,
		Round:  3,
		Lambda: []byte("Lambda"),
		Value:  []byte("value"),
	}))
	instance.commitMessages.AddMessage(ibfttesting.SignMsg(1, sks[1], &proto.Message{
		Type:   proto.RoundState_Commit,
		Round:  3,
		Lambda: []byte("Lambda"),
		Value:  []byte("value"),
	}))
	instance.commitMessages.AddMessage(ibfttesting.SignMsg(2, sks[2], &proto.Message{
		Type:   proto.RoundState_Commit,
		Round:  3,
		Lambda: []byte("Lambda"),
		Value:  []byte("value"),
	}))

	// test aggregation
	msg, err := instance.CommittedAggregatedMsg()
	require.NoError(t, err)
	require.ElementsMatch(t, []uint64{0, 1, 2}, msg.SignerIds)

	// test that doesn't aggregate different value
	instance.commitMessages.AddMessage(ibfttesting.SignMsg(3, sks[3], &proto.Message{
		Type:   proto.RoundState_Commit,
		Round:  3,
		Lambda: []byte("Lambda"),
		Value:  []byte("value2"),
	}))
	msg, err = instance.CommittedAggregatedMsg()
	require.NoError(t, err)
	require.ElementsMatch(t, []uint64{0, 1, 2}, msg.SignerIds)
}
