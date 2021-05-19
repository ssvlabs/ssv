package ibft

import (
	"github.com/bloxapp/ssv/ibft/leader"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"go.uber.org/zap/zaptest"

	msgcontinmem "github.com/bloxapp/ssv/ibft/msgcont/inmem"
	"github.com/bloxapp/ssv/ibft/proto"
	"github.com/bloxapp/ssv/utils/dataval/bytesval"
)

func TestJustifyPrePrepareAfterChangeRoundPrepared(t *testing.T) {
	secretKeys, nodes := GenerateNodes(4)
	value := []byte(time.Now().Weekday().String())
	instance := &Instance{
		PrePrepareMessages:  msgcontinmem.New(3),
		ChangeRoundMessages: msgcontinmem.New(3),
		Params: &proto.InstanceParams{
			ConsensusParams: proto.DefaultConsensusParams(),
			IbftCommittee:   nodes,
		},
		State: &proto.State{
			Round:         1,
			Lambda:        []byte("Lambda"),
			PreparedRound: 0,
			PreparedValue: nil,
		},
		Me: &proto.Node{
			IbftId: 1,
			Pk:     nodes[1].Pk,
			Sk:     secretKeys[1].Serialize(),
		},
		ValueCheck:     bytesval.New(value),
		LeaderSelector: &leader.Constant{},
		Logger:         zaptest.NewLogger(t),
	}

	// change round no quorum
	msg := SignMsg(t, 1, secretKeys[1], &proto.Message{
		Type:   proto.RoundState_ChangeRound,
		Round:  2,
		Lambda: []byte("Lambda"),
		Value: changeRoundDataToBytes(&proto.ChangeRoundData{
			PreparedRound: 1,
			PreparedValue: value,
		}),
	})
	instance.ChangeRoundMessages.AddMessage(msg)

	// no quorum achieved, err
	msg = SignMsg(t, 1, secretKeys[1], &proto.Message{
		Type:   proto.RoundState_PrePrepare,
		Round:  2,
		Lambda: []byte("Lambda"),
		Value:  value,
	})
	instance.PrePrepareMessages.AddMessage(msg)
	res, err := instance.JustifyPrePrepare(2)
	require.False(t, res)
	require.NoError(t, err)

	// test justified change round
	msg = SignMsg(t, 2, secretKeys[2], &proto.Message{
		Type:   proto.RoundState_ChangeRound,
		Round:  2,
		Lambda: []byte("Lambda"),
		Value: changeRoundDataToBytes(&proto.ChangeRoundData{
			PreparedRound: 1,
			PreparedValue: value,
		}),
	})
	instance.ChangeRoundMessages.AddMessage(msg)
	msg = SignMsg(t, 3, secretKeys[3], &proto.Message{
		Type:   proto.RoundState_ChangeRound,
		Round:  2,
		Lambda: []byte("Lambda"),
		Value: changeRoundDataToBytes(&proto.ChangeRoundData{
			PreparedRound: 1,
			PreparedValue: value,
		}),
	})
	instance.ChangeRoundMessages.AddMessage(msg)

	res, err = instance.JustifyPrePrepare(2)
	require.True(t, res)
	require.NoError(t, err)
}

func TestJustifyPrePrepareAfterChangeRoundNoPrepare(t *testing.T) {
	secretKeys, nodes := GenerateNodes(4)
	value := []byte(time.Now().Weekday().String())
	instance := &Instance{
		PrePrepareMessages:  msgcontinmem.New(3),
		PrepareMessages:     msgcontinmem.New(3),
		ChangeRoundMessages: msgcontinmem.New(3),
		Params: &proto.InstanceParams{
			ConsensusParams: proto.DefaultConsensusParams(),
			IbftCommittee:   nodes,
		},
		State: &proto.State{
			Round:         1,
			Lambda:        []byte("Lambda"),
			PreparedRound: 0,
			PreparedValue: nil,
		},
		Me: &proto.Node{
			IbftId: 1,
			Pk:     nodes[1].Pk,
			Sk:     secretKeys[1].Serialize(),
		},
		ValueCheck:     bytesval.New(value),
		LeaderSelector: &leader.Constant{},
		Logger:         zaptest.NewLogger(t),
	}

	// change round no quorum
	msg := SignMsg(t, 1, secretKeys[1], &proto.Message{
		Type:   proto.RoundState_ChangeRound,
		Round:  2,
		Lambda: []byte("Lambda"),
		Value:  changeRoundDataToBytes(&proto.ChangeRoundData{}),
	})
	instance.ChangeRoundMessages.AddMessage(msg)

	msg = SignMsg(t, 2, secretKeys[2], &proto.Message{
		Type:   proto.RoundState_ChangeRound,
		Round:  2,
		Lambda: []byte("Lambda"),
		Value:  changeRoundDataToBytes(&proto.ChangeRoundData{}),
	})
	instance.ChangeRoundMessages.AddMessage(msg)

	// no quorum achieved, can't justify
	res, err := instance.JustifyPrePrepare(2)
	require.False(t, res)
	require.NoError(t, err)

	// test justified change round
	msg = SignMsg(t, 3, secretKeys[3], &proto.Message{
		Type:   proto.RoundState_ChangeRound,
		Round:  2,
		Lambda: []byte("Lambda"),
		Value:  changeRoundDataToBytes(&proto.ChangeRoundData{}),
	})
	instance.ChangeRoundMessages.AddMessage(msg)

	// quorum achieved, can justify
	res, err = instance.JustifyPrePrepare(2)
	require.True(t, res)
	require.NoError(t, err)
}

func TestUponPrePrepareHappyFlow(t *testing.T) {
	secretKeys, nodes := GenerateNodes(4)
	instance := &Instance{
		PrePrepareMessages: msgcontinmem.New(3),
		PrepareMessages:    msgcontinmem.New(3),
		Params: &proto.InstanceParams{
			ConsensusParams: proto.DefaultConsensusParams(),
			IbftCommittee:   nodes,
		},
		State: &proto.State{
			Round:         1,
			Lambda:        []byte("Lambda"),
			PreparedRound: 0,
			PreparedValue: nil,
		},
		Me: &proto.Node{
			IbftId: 1,
			Pk:     nodes[1].Pk,
			Sk:     secretKeys[1].Serialize(),
		},
		ValueCheck:     bytesval.New([]byte(time.Now().Weekday().String())),
		LeaderSelector: &leader.Constant{},
		Logger:         zaptest.NewLogger(t),
	}

	// test happy flow
	msg := SignMsg(t, 2, secretKeys[2], &proto.Message{
		Type:   proto.RoundState_PrePrepare,
		Round:  1,
		Lambda: []byte("Lambda"),
		Value:  []byte(time.Now().Weekday().String()),
	})
	err := instance.UponPrePrepareMsg().Run(msg)
	require.NoError(t, err)
	msgs := instance.PrePrepareMessages.ReadOnlyMessagesByRound(1)
	require.NotNil(t, msgs[0])
	require.True(t, instance.Stage() == proto.RoundState_PrePrepare)

	// return nil if another pre-prepare received.
	err = instance.UponPrePrepareMsg().Run(msg)
	require.NoError(t, err)
}

func TestInstance_JustifyPrePrepare(t *testing.T) {
	secretKeys, nodes := GenerateNodes(4)
	instance := &Instance{
		ChangeRoundMessages: msgcontinmem.New(3),
		Params: &proto.InstanceParams{
			ConsensusParams: proto.DefaultConsensusParams(),
			IbftCommittee:   nodes,
		},
		State: &proto.State{
			Round:         1,
			PreparedRound: 0,
			PreparedValue: nil,
		},
	}

	res, err := instance.JustifyPrePrepare(1)
	require.NoError(t, err)
	require.True(t, res)

	// try to justify round 2 without round change
	instance.State.Round = 2
	res, err = instance.JustifyPrePrepare(2)
	require.NoError(t, err)
	require.False(t, res)

	// test no change round quorum
	msg := &proto.Message{
		Type:   proto.RoundState_ChangeRound,
		Round:  2,
		Lambda: []byte("lambdas"),
		Value:  changeRoundDataToBytes(&proto.ChangeRoundData{}),
	}
	instance.ChangeRoundMessages.AddMessage(SignMsg(t, 1, secretKeys[1], msg))

	msg = &proto.Message{
		Type:   proto.RoundState_ChangeRound,
		Round:  2,
		Lambda: []byte("lambdas"),
		Value:  changeRoundDataToBytes(&proto.ChangeRoundData{}),
	}
	instance.ChangeRoundMessages.AddMessage(SignMsg(t, 2, secretKeys[2], msg))

	res, err = instance.JustifyPrePrepare(2)
	require.NoError(t, err)
	require.False(t, res)

	// test with quorum of change round
	msg = &proto.Message{
		Type:   proto.RoundState_ChangeRound,
		Round:  2,
		Lambda: []byte("lambdas"),
		Value:  changeRoundDataToBytes(&proto.ChangeRoundData{}),
	}
	instance.ChangeRoundMessages.AddMessage(SignMsg(t, 3, secretKeys[3], msg))

	res, err = instance.JustifyPrePrepare(2)
	require.NoError(t, err)
	require.True(t, res)
}

func TestPrePreparePipeline(t *testing.T) {
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
	pipeline := instance.prePrepareMsgPipeline()
	require.EqualValues(t, "combination of: type check, lambda, round, validator PK, sequence, authorize, validate pre-prepare, upon pre-prepare msg, ", pipeline.Name())
}
