package ibft

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/bloxapp/ssv/ibft/msgcont"
	"github.com/bloxapp/ssv/ibft/proto"
)

func TestCommittedAggregatedMsg(t *testing.T) {
	sks, nodes := generateNodes(4)
	instance := &Instance{
		commitMessages: msgcont.NewMessagesContainer(),
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
	instance.commitMessages.AddMessage(signMsg(0, sks[0], &proto.Message{
		Type:   proto.RoundState_Commit,
		Round:  3,
		Lambda: []byte("Lambda"),
		Value:  []byte("value"),
	}))
	instance.commitMessages.AddMessage(signMsg(1, sks[1], &proto.Message{
		Type:   proto.RoundState_Commit,
		Round:  3,
		Lambda: []byte("Lambda"),
		Value:  []byte("value"),
	}))
	instance.commitMessages.AddMessage(signMsg(2, sks[2], &proto.Message{
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
	instance.commitMessages.AddMessage(signMsg(3, sks[3], &proto.Message{
		Type:   proto.RoundState_Commit,
		Round:  3,
		Lambda: []byte("Lambda"),
		Value:  []byte("value2"),
	}))
	msg, err = instance.CommittedAggregatedMsg()
	require.NoError(t, err)
	require.ElementsMatch(t, []uint64{0, 1, 2}, msg.SignerIds)
}
