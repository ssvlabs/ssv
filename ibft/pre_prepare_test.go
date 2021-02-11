package ibft

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"go.uber.org/zap/zaptest"

	"github.com/bloxapp/ssv/ibft/msgcont"
	"github.com/bloxapp/ssv/ibft/proto"
	"github.com/bloxapp/ssv/utils/validator/bytesval"
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
			Lambda:        []byte("Lambda"),
			PreparedRound: 0,
			PreparedValue: nil,
		},
		Me: &proto.Node{
			IbftId: 0,
			Pk:     nodes[0].Pk,
			Sk:     secretKeys[0].Serialize(),
		},
		consensus: bytesval.New([]byte(time.Now().Weekday().String())),
		logger:    zaptest.NewLogger(t),
	}

	// change round no quorum
	msg := signMsg(0, secretKeys[0], &proto.Message{
		Type:   proto.RoundState_ChangeRound,
		Round:  2,
		Lambda: []byte("Lambda"),
		Value: changeRoundDataToBytes(&proto.ChangeRoundData{
			PreparedRound: 1,
			PreparedValue: []byte(time.Now().Weekday().String()),
		}),
	})
	instance.changeRoundMessages.AddMessage(msg)

	msg = signMsg(0, secretKeys[0], &proto.Message{
		Type:   proto.RoundState_ChangeRound,
		Round:  2,
		Lambda: []byte("Lambda"),
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
		Lambda: []byte("Lambda"),
		Value:  []byte(time.Now().Weekday().String()),
	})
	require.EqualError(t, instance.uponPrePrepareMsg()(msg), "received un-justified pre-prepare message")

	// test justified change round
	msg = signMsg(0, secretKeys[0], &proto.Message{
		Type:   proto.RoundState_ChangeRound,
		Round:  2,
		Lambda: []byte("Lambda"),
		Value: changeRoundDataToBytes(&proto.ChangeRoundData{
			PreparedRound: 1,
			PreparedValue: []byte(time.Now().Weekday().String()),
		}),
	})
	instance.changeRoundMessages.AddMessage(msg)

	msg = signMsg(2, secretKeys[2], &proto.Message{
		Type:   proto.RoundState_PrePrepare,
		Round:  2,
		Lambda: []byte("Lambda"),
		Value:  []byte(time.Now().Weekday().String()),
	})
	require.NoError(t, instance.uponPrePrepareMsg()(msg))
}

func TestUponPrePrepareAfterChangeRoundNoPrepare(t *testing.T) {
	secretKeys, nodes := generateNodes(4)
	instance := &Instance{
		prePrepareMessages:  msgcont.NewMessagesContainer(),
		prepareMessages:     msgcont.NewMessagesContainer(),
		changeRoundMessages: msgcont.NewMessagesContainer(),
		params: &proto.InstanceParams{
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
		consensus: bytesval.New([]byte(time.Now().Weekday().String())),
		logger:    zaptest.NewLogger(t),
	}

	// change round no quorum
	msg := signMsg(0, secretKeys[0], &proto.Message{
		Type:   proto.RoundState_ChangeRound,
		Round:  2,
		Lambda: []byte("Lambda"),
	})
	instance.changeRoundMessages.AddMessage(msg)

	msg = signMsg(1, secretKeys[1], &proto.Message{
		Type:   proto.RoundState_ChangeRound,
		Round:  2,
		Lambda: []byte("Lambda"),
	})
	instance.changeRoundMessages.AddMessage(msg)

	// no quorum achieved, err
	require.EqualError(t, instance.uponPrePrepareMsg()(msg), "received un-justified pre-prepare message")

	// test justified change round
	msg = signMsg(2, secretKeys[2], &proto.Message{
		Type:   proto.RoundState_ChangeRound,
		Round:  2,
		Lambda: []byte("Lambda"),
	})
	instance.changeRoundMessages.AddMessage(msg)
	require.NoError(t, instance.uponPrePrepareMsg()(msg))
}

func TestUponPrePrepareHappyFlow(t *testing.T) {
	secretKeys, nodes := generateNodes(4)
	instance := &Instance{
		prePrepareMessages: msgcont.NewMessagesContainer(),
		prepareMessages:    msgcont.NewMessagesContainer(),
		params: &proto.InstanceParams{
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
		consensus: bytesval.New([]byte(time.Now().Weekday().String())),
		logger:    zaptest.NewLogger(t),
	}

	// test happy flow
	msg := signMsg(1, secretKeys[1], &proto.Message{
		Type:   proto.RoundState_PrePrepare,
		Round:  1,
		Lambda: []byte("Lambda"),
		Value:  []byte(time.Now().Weekday().String()),
	})
	err := instance.uponPrePrepareMsg()(msg)
	require.NoError(t, err)
	msgs := instance.prePrepareMessages.ReadOnlyMessagesByRound(1)
	require.NotNil(t, msgs[1])
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
			Lambda:        []byte("Lambda"),
			PreparedRound: 0,
			PreparedValue: nil,
		},
		consensus: bytesval.New([]byte(time.Now().Weekday().String())),
	}

	// test no signer
	msg := &proto.SignedMessage{
		Message: &proto.Message{
			Type:   proto.RoundState_PrePrepare,
			Round:  1,
			Lambda: []byte("Lambda"),
			Value:  []byte(time.Now().Weekday().String()),
		},
		Signature: []byte{},
		SignerIds: []uint64{},
	}
	err := i.validatePrePrepareMsg()(msg)
	require.EqualError(t, err, "invalid number of signers for pre-prepare message")

	// test > 1 signer
	msg = &proto.SignedMessage{
		Message: &proto.Message{
			Type:   proto.RoundState_PrePrepare,
			Round:  1,
			Lambda: []byte("Lambda"),
			Value:  []byte(time.Now().Weekday().String()),
		},
		Signature: []byte{},
		SignerIds: []uint64{1, 2},
	}
	err = i.validatePrePrepareMsg()(msg)
	require.EqualError(t, err, "invalid number of signers for pre-prepare message")

	msg = signMsg(1, sks[1], &proto.Message{
		Type:   proto.RoundState_PrePrepare,
		Round:  1,
		Lambda: []byte("Lambda"),
		Value:  []byte("wrong value"),
	})
	err = i.validatePrePrepareMsg()(msg)
	require.EqualError(t, err, "msg value is wrong")

	msg = signMsg(2, sks[2], &proto.Message{
		Type:   proto.RoundState_PrePrepare,
		Round:  1,
		Lambda: []byte("Lambda"),
		Value:  []byte("wrong value"),
	})
	err = i.validatePrePrepareMsg()(msg)
	require.EqualError(t, err, "pre-prepare message sender is not the round's leader")

	msg = signMsg(1, sks[1], &proto.Message{
		Type:   proto.RoundState_PrePrepare,
		Round:  1,
		Lambda: []byte("Lambda"),
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
