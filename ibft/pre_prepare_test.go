package ibft

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/bloxapp/ssv/ibft/msgcont"
	"github.com/bloxapp/ssv/ibft/proto"
)

func TestInstance_JustifyPrePrepare(t *testing.T) {
	sks, nodes := generateNodes(4)
	i := &Instance{
		changeRoundMessages: msgcont.NewMessagesContainer(),
		params: &proto.InstanceParams{
			ConsensusParams: proto.DefaultConsensusParams(),
			IbftCommittee:   nodes,
		},
		state: &State{
			Round:         1,
			PreparedRound: 0,
			PreparedValue: nil,
		},
	}

	res, err := i.JustifyPrePrepare(1)
	require.NoError(t, err)
	require.True(t, res)

	// test no change round quorum
	msg := &proto.Message{
		Type:   proto.RoundState_ChangeRound,
		Round:  2,
		Lambda: []byte("lambdas"),
	}
	i.changeRoundMessages.AddMessage(signMsg(0, sks[0], msg))

	msg = &proto.Message{
		Type:   proto.RoundState_ChangeRound,
		Round:  2,
		Lambda: []byte("lambdas"),
	}
	i.changeRoundMessages.AddMessage(signMsg(1, sks[1], msg))

	res, err = i.JustifyPrePrepare(2)
	require.NoError(t, err)
	require.False(t, res)

	// test with quorum of change round
	msg = &proto.Message{
		Type:   proto.RoundState_ChangeRound,
		Round:  2,
		Lambda: []byte("lambdas"),
	}
	i.changeRoundMessages.AddMessage(signMsg(2, sks[2], msg))

	res, err = i.JustifyPrePrepare(2)
	require.NoError(t, err)
	require.True(t, res)
}
