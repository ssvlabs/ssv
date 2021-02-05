package ibft

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/bloxapp/ssv/ibft/msgcont"
	"github.com/bloxapp/ssv/ibft/proto"
	"github.com/bloxapp/ssv/ibft/val/weekday"
)

func TestUponPrePrepareAfterChangeRoundPrepared(t *testing.T) {
	secretKeys, nodes := generateNodes(4)
	instance := &Instance{
		prePrepareMessages:  msgcont.NewMessagesContainer(),
		changeRoundMessages: msgcont.NewMessagesContainer(),
		params: &proto.InstanceParams{
			ConsensusParams: proto.DefaultConsensusParams(),
			IbftCommittee:   nodes,
		},
		State: &proto.State{
			Round:         1,
			Lambda:        []byte("lambda"),
			PreparedRound: 0,
			PreparedValue: nil,
		},
		Me: &proto.Node{
			IbftId: 0,
			Pk:     nodes[0].Pk,
			Sk:     secretKeys[0].Serialize(),
		},
		consensus: weekday.New(),
	}

	// change round no quorum
	msg := signMsg(0, secretKeys[0], &proto.Message{
		Type:   proto.RoundState_ChangeRound,
		Round:  2,
		Lambda: []byte("lambda"),
		Value: changeRoundDataToBytes(&proto.ChangeRoundData{
			PreparedRound: 1,
			PreparedValue: []byte(time.Now().Weekday().String()),
		}),
	})
	instance.changeRoundMessages.AddMessage(msg)

	msg = signMsg(0, secretKeys[0], &proto.Message{
		Type:   proto.RoundState_ChangeRound,
		Round:  2,
		Lambda: []byte("lambda"),
		Value: changeRoundDataToBytes(&proto.ChangeRoundData{
			PreparedRound: 1,
			PreparedValue: []byte(time.Now().Weekday().String()),
		}),
	})
	instance.changeRoundMessages.AddMessage(msg)

	// no quorum achieved, err
	msg = signMsg(2, secretKeys[2], &proto.Message{
		Type:   proto.RoundState_PrePrepare,
		Round:  2,
		Lambda: []byte("lambda"),
		Value:  []byte(time.Now().Weekday().String()),
	})
	require.EqualError(t, instance.uponPrePrepareMsg()(msg), "received un-justified pre-prepare message")

	// test justified change round
	msg = signMsg(0, secretKeys[0], &proto.Message{
		Type:   proto.RoundState_ChangeRound,
		Round:  2,
		Lambda: []byte("lambda"),
		Value: changeRoundDataToBytes(&proto.ChangeRoundData{
			PreparedRound: 1,
			PreparedValue: []byte(time.Now().Weekday().String()),
		}),
	})
	instance.changeRoundMessages.AddMessage(msg)

	msg = signMsg(2, secretKeys[2], &proto.Message{
		Type:   proto.RoundState_PrePrepare,
		Round:  2,
		Lambda: []byte("lambda"),
		Value:  []byte(time.Now().Weekday().String()),
	})
	require.NoError(t, instance.uponPrePrepareMsg()(msg))
}

func TestUponPrePrepareAfterChangeRoundNoPrepare(t *testing.T) {
	secretKeys, nodes := generateNodes(4)
	instance := &Instance{
		prePrepareMessages:  msgcont.NewMessagesContainer(),
		changeRoundMessages: msgcont.NewMessagesContainer(),
		params: &proto.InstanceParams{
			ConsensusParams: proto.DefaultConsensusParams(),
			IbftCommittee:   nodes,
		},
		State: &proto.State{
			Round:         1,
			Lambda:        []byte("lambda"),
			PreparedRound: 0,
			PreparedValue: nil,
		},
		Me: &proto.Node{
			IbftId: 0,
			Pk:     nodes[0].Pk,
			Sk:     secretKeys[0].Serialize(),
		},
		consensus: weekday.New(),
	}

	// change round no quorum
	msg := signMsg(0, secretKeys[0], &proto.Message{
		Type:   proto.RoundState_ChangeRound,
		Round:  2,
		Lambda: []byte("lambda"),
	})
	instance.changeRoundMessages.AddMessage(msg)

	msg = signMsg(1, secretKeys[1], &proto.Message{
		Type:   proto.RoundState_ChangeRound,
		Round:  2,
		Lambda: []byte("lambda"),
	})
	instance.changeRoundMessages.AddMessage(msg)

	// no quorum achieved, err
	require.EqualError(t, instance.uponPrePrepareMsg()(msg), "received un-justified pre-prepare message")

	// test justified change round
	msg = signMsg(2, secretKeys[2], &proto.Message{
		Type:   proto.RoundState_ChangeRound,
		Round:  2,
		Lambda: []byte("lambda"),
	})
	instance.changeRoundMessages.AddMessage(msg)
	require.NoError(t, instance.uponPrePrepareMsg()(msg))
}

func TestUponPrePrepareHappyFlow(t *testing.T) {
	secretKeys, nodes := generateNodes(4)
	instance := &Instance{
		prePrepareMessages: msgcont.NewMessagesContainer(),
		params: &proto.InstanceParams{
			ConsensusParams: proto.DefaultConsensusParams(),
			IbftCommittee:   nodes,
		},
		State: &proto.State{
			Round:         1,
			Lambda:        []byte("lambda"),
			PreparedRound: 0,
			PreparedValue: nil,
		},
		Me: &proto.Node{
			IbftId: 0,
			Pk:     nodes[0].Pk,
			Sk:     secretKeys[0].Serialize(),
		},
		consensus: weekday.New(),
	}

	// test happy flow
	msg := signMsg(1, secretKeys[1], &proto.Message{
		Type:   proto.RoundState_PrePrepare,
		Round:  1,
		Lambda: []byte("lambda"),
		Value:  []byte(time.Now().Weekday().String()),
	})
	err := instance.uponPrePrepareMsg()(msg)
	require.NoError(t, err)
	msgs := instance.prePrepareMessages.ReadOnlyMessagesByRound(1)
	require.NotNil(t, msgs[0])
	require.True(t, instance.State.Stage == proto.RoundState_PrePrepare)

	// return nil if another pre-prepare received.
	err = instance.uponPrePrepareMsg()(msg)
	require.NoError(t, err)
}

func TestValidatePrePrepareValue(t *testing.T) {
	sks, nodes := generateNodes(4)
	i := &Instance{
		prePrepareMessages: msgcont.NewMessagesContainer(),
		params: &proto.InstanceParams{
			ConsensusParams: proto.DefaultConsensusParams(),
			IbftCommittee:   nodes,
		},
		State: &proto.State{
			Round:         1,
			Lambda:        []byte("lambda"),
			PreparedRound: 0,
			PreparedValue: nil,
		},
		consensus: weekday.New(),
	}

	msg := signMsg(1, sks[1], &proto.Message{
		Type:   proto.RoundState_PrePrepare,
		Round:  1,
		Lambda: []byte("lambda"),
		Value:  []byte("wrong value"),
	})
	err := i.validatePrePrepareMsg()(msg)
	require.EqualError(t, err, "message value is wrong")

	msg = signMsg(2, sks[2], &proto.Message{
		Type:   proto.RoundState_PrePrepare,
		Round:  1,
		Lambda: []byte("lambda"),
		Value:  []byte("wrong value"),
	})
	err = i.validatePrePrepareMsg()(msg)
	require.EqualError(t, err, "pre-prepare message sender is not the round's leader")

	msg = signMsg(1, sks[1], &proto.Message{
		Type:   proto.RoundState_PrePrepare,
		Round:  1,
		Lambda: []byte("lambda"),
		Value:  []byte(time.Now().Weekday().String()),
	})
	err = i.validatePrePrepareMsg()(msg)
	require.NoError(t, err)
}

func TestInstance_JustifyPrePrepare(t *testing.T) {
	secretKeys, nodes := generateNodes(4)
	instance := &Instance{
		changeRoundMessages: msgcont.NewMessagesContainer(),
		params: &proto.InstanceParams{
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
	instance.changeRoundMessages.AddMessage(signMsg(0, secretKeys[0], msg))

	msg = &proto.Message{
		Type:   proto.RoundState_ChangeRound,
		Round:  2,
		Lambda: []byte("lambdas"),
	}
	instance.changeRoundMessages.AddMessage(signMsg(1, secretKeys[1], msg))

	res, err = instance.JustifyPrePrepare(2)
	require.NoError(t, err)
	require.False(t, res)

	// test with quorum of change round
	msg = &proto.Message{
		Type:   proto.RoundState_ChangeRound,
		Round:  2,
		Lambda: []byte("lambdas"),
	}
	instance.changeRoundMessages.AddMessage(signMsg(2, secretKeys[2], msg))

	res, err = instance.JustifyPrePrepare(2)
	require.NoError(t, err)
	require.True(t, res)
}
