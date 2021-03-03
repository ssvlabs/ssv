package ibft

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"go.uber.org/zap/zaptest"

	msgcontinmem "github.com/bloxapp/ssv/ibft/msgcont/inmem"
	"github.com/bloxapp/ssv/ibft/proto"
	ibfttesting "github.com/bloxapp/ssv/ibft/spec_testing"
	"github.com/bloxapp/ssv/utils/dataval/bytesval"
)

func TestUponPrePrepareAfterChangeRoundPrepared(t *testing.T) {
	secretKeys, nodes := ibfttesting.GenerateNodes(4)
	instance := &Instance{
		PrePrepareMessages:  msgcontinmem.New(),
		ChangeRoundMessages: msgcontinmem.New(),
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
			IbftId: 0,
			Pk:     nodes[0].Pk,
			Sk:     secretKeys[0].Serialize(),
		},
		Consensus: bytesval.New([]byte(time.Now().Weekday().String())),
		Logger:    zaptest.NewLogger(t),
	}

	// change round no quorum
	msg := ibfttesting.SignMsg(0, secretKeys[0], &proto.Message{
		Type:   proto.RoundState_ChangeRound,
		Round:  2,
		Lambda: []byte("Lambda"),
		Value: changeRoundDataToBytes(&proto.ChangeRoundData{
			PreparedRound: 1,
			PreparedValue: []byte(time.Now().Weekday().String()),
		}),
	})
	instance.ChangeRoundMessages.AddMessage(msg)

	msg = ibfttesting.SignMsg(0, secretKeys[0], &proto.Message{
		Type:   proto.RoundState_ChangeRound,
		Round:  2,
		Lambda: []byte("Lambda"),
		Value: changeRoundDataToBytes(&proto.ChangeRoundData{
			PreparedRound: 1,
			PreparedValue: []byte(time.Now().Weekday().String()),
		}),
	})
	instance.ChangeRoundMessages.AddMessage(msg)

	// no quorum achieved, err
	msg = ibfttesting.SignMsg(2, secretKeys[2], &proto.Message{
		Type:   proto.RoundState_PrePrepare,
		Round:  2,
		Lambda: []byte("Lambda"),
		Value:  []byte(time.Now().Weekday().String()),
	})
	require.EqualError(t, instance.UponPrePrepareMsg().Run(msg), "received un-justified pre-prepare message")

	// test justified change round
	msg = ibfttesting.SignMsg(0, secretKeys[0], &proto.Message{
		Type:   proto.RoundState_ChangeRound,
		Round:  2,
		Lambda: []byte("Lambda"),
		Value: changeRoundDataToBytes(&proto.ChangeRoundData{
			PreparedRound: 1,
			PreparedValue: []byte(time.Now().Weekday().String()),
		}),
	})
	instance.ChangeRoundMessages.AddMessage(msg)

	msg = ibfttesting.SignMsg(2, secretKeys[2], &proto.Message{
		Type:   proto.RoundState_PrePrepare,
		Round:  2,
		Lambda: []byte("Lambda"),
		Value:  []byte(time.Now().Weekday().String()),
	})
	require.NoError(t, instance.UponPrePrepareMsg().Run(msg))
}

func TestUponPrePrepareAfterChangeRoundNoPrepare(t *testing.T) {
	secretKeys, nodes := ibfttesting.GenerateNodes(4)
	instance := &Instance{
		PrePrepareMessages:  msgcontinmem.New(),
		PrepareMessages:     msgcontinmem.New(),
		ChangeRoundMessages: msgcontinmem.New(),
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
			IbftId: 0,
			Pk:     nodes[0].Pk,
			Sk:     secretKeys[0].Serialize(),
		},
		Consensus: bytesval.New([]byte(time.Now().Weekday().String())),
		Logger:    zaptest.NewLogger(t),
	}

	// change round no quorum
	msg := ibfttesting.SignMsg(0, secretKeys[0], &proto.Message{
		Type:   proto.RoundState_ChangeRound,
		Round:  2,
		Lambda: []byte("Lambda"),
	})
	instance.ChangeRoundMessages.AddMessage(msg)

	msg = ibfttesting.SignMsg(1, secretKeys[1], &proto.Message{
		Type:   proto.RoundState_ChangeRound,
		Round:  2,
		Lambda: []byte("Lambda"),
	})
	instance.ChangeRoundMessages.AddMessage(msg)

	// no quorum achieved, err
	require.EqualError(t, instance.UponPrePrepareMsg().Run(msg), "received un-justified pre-prepare message")

	// test justified change round
	msg = ibfttesting.SignMsg(2, secretKeys[2], &proto.Message{
		Type:   proto.RoundState_ChangeRound,
		Round:  2,
		Lambda: []byte("Lambda"),
	})
	instance.ChangeRoundMessages.AddMessage(msg)
	require.NoError(t, instance.UponPrePrepareMsg().Run(msg))
}

func TestUponPrePrepareHappyFlow(t *testing.T) {
	secretKeys, nodes := ibfttesting.GenerateNodes(4)
	instance := &Instance{
		PrePrepareMessages: msgcontinmem.New(),
		PrepareMessages:    msgcontinmem.New(),
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
			IbftId: 0,
			Pk:     nodes[0].Pk,
			Sk:     secretKeys[0].Serialize(),
		},
		Consensus: bytesval.New([]byte(time.Now().Weekday().String())),
		Logger:    zaptest.NewLogger(t),
	}

	// test happy flow
	msg := ibfttesting.SignMsg(1, secretKeys[1], &proto.Message{
		Type:   proto.RoundState_PrePrepare,
		Round:  1,
		Lambda: []byte("Lambda"),
		Value:  []byte(time.Now().Weekday().String()),
	})
	err := instance.UponPrePrepareMsg().Run(msg)
	require.NoError(t, err)
	msgs := instance.PrePrepareMessages.ReadOnlyMessagesByRound(1)
	require.NotNil(t, msgs[1])
	require.True(t, instance.State.Stage == proto.RoundState_PrePrepare)

	// return nil if another pre-prepare received.
	err = instance.UponPrePrepareMsg().Run(msg)
	require.NoError(t, err)
}

func TestInstance_JustifyPrePrepare(t *testing.T) {
	secretKeys, nodes := ibfttesting.GenerateNodes(4)
	instance := &Instance{
		ChangeRoundMessages: msgcontinmem.New(),
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

	// test no change round quorum
	msg := &proto.Message{
		Type:   proto.RoundState_ChangeRound,
		Round:  2,
		Lambda: []byte("lambdas"),
	}
	instance.ChangeRoundMessages.AddMessage(ibfttesting.SignMsg(0, secretKeys[0], msg))

	msg = &proto.Message{
		Type:   proto.RoundState_ChangeRound,
		Round:  2,
		Lambda: []byte("lambdas"),
	}
	instance.ChangeRoundMessages.AddMessage(ibfttesting.SignMsg(1, secretKeys[1], msg))

	res, err = instance.JustifyPrePrepare(2)
	require.NoError(t, err)
	require.False(t, res)

	// test with quorum of change round
	msg = &proto.Message{
		Type:   proto.RoundState_ChangeRound,
		Round:  2,
		Lambda: []byte("lambdas"),
	}
	instance.ChangeRoundMessages.AddMessage(ibfttesting.SignMsg(2, secretKeys[2], msg))

	res, err = instance.JustifyPrePrepare(2)
	require.NoError(t, err)
	require.True(t, res)
}
