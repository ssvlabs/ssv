package ibft

import (
	"github.com/bloxapp/ssv/ibft/proto"
	"github.com/bloxapp/ssv/network"
	"github.com/bloxapp/ssv/validator/storage"
	"github.com/stretchr/testify/require"
	"testing"
)

func TestGrouping(t *testing.T) {
	sks, nodes := GenerateNodes(4)
	msgs := []*network.Message{
		{
			SignedMessage: SignMsg(t, 1, sks[1], &proto.Message{
				Type:        proto.RoundState_ChangeRound,
				Round:       1,
				SeqNumber:   1,
				Lambda:      []byte("lambda"),
				ValidatorPk: sks[1].GetPublicKey().Serialize(),
				Value: changeRoundDataToBytes(&proto.ChangeRoundData{
					PreparedValue: nil,
				}),
			}),
		},
		{
			SignedMessage: SignMsg(t, 1, sks[1], &proto.Message{
				Type:        proto.RoundState_ChangeRound,
				Round:       1,
				SeqNumber:   1,
				Lambda:      []byte("lambda"),
				ValidatorPk: sks[1].GetPublicKey().Serialize(),
				Value: changeRoundDataToBytes(&proto.ChangeRoundData{
					PreparedValue: nil,
				}),
			}),
		},
		{
			SignedMessage: SignMsg(t, 1, sks[1], &proto.Message{
				Type:        proto.RoundState_ChangeRound,
				Round:       2,
				SeqNumber:   1,
				Lambda:      []byte("lambda"),
				ValidatorPk: sks[1].GetPublicKey().Serialize(),
				Value: changeRoundDataToBytes(&proto.ChangeRoundData{
					PreparedValue: nil,
				}),
			}),
		},
	}

	instance := &Instance{
		State: &proto.State{
			Lambda:    []byte("lambda"),
			SeqNumber: 1,
		},
		Config: proto.DefaultConsensusParams(),
		ValidatorShare: &storage.Share{
			Committee: nodes,
			PublicKey: sks[1].GetPublicKey(),
		},
	}

	res := instance.groupPartialChangeRoundMsgs(msgs)
	require.Len(t, res[0], 0)
	require.Len(t, res[1], 2)
	require.Len(t, res[2], 1)
}

func TestFindPartialChangeRound(t *testing.T) {
	tests := []struct {
		name           string
		msgs           map[uint64][]*proto.SignedMessage
		expectedFound  bool
		expectedLowest uint64
	}{
		{
			"lowest 4",
			map[uint64][]*proto.SignedMessage{
				4: {
					&proto.SignedMessage{},
					&proto.SignedMessage{},
				},
			},
			true,
			4,
		},
		{
			"lowest is lower than state round",
			map[uint64][]*proto.SignedMessage{
				0: {
					&proto.SignedMessage{},
					&proto.SignedMessage{},
				},
			},
			false,
			100000,
		},
		{
			"lowest 4",
			map[uint64][]*proto.SignedMessage{
				4: {
					&proto.SignedMessage{},
					&proto.SignedMessage{},
				},
				10: {
					&proto.SignedMessage{},
					&proto.SignedMessage{},
				},
			},
			true,
			4,
		},
		{
			"not found",
			map[uint64][]*proto.SignedMessage{
				4: {
					&proto.SignedMessage{},
				},
				5: {
					&proto.SignedMessage{},
				},
			},
			false,
			100000,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(tt *testing.T) {
			instance := &Instance{
				State:  &proto.State{Round: 1},
				Config: proto.DefaultConsensusParams(),
				ValidatorShare: &storage.Share{Committee: map[uint64]*proto.Node{
					0: {IbftId: 0},
					1: {IbftId: 1},
					2: {IbftId: 2},
					3: {IbftId: 3},
				}},
			}

			found, lowest := instance.findPartialQuorum(test.msgs)
			require.EqualValues(tt, test.expectedFound, found)
			require.EqualValues(tt, test.expectedLowest, lowest)
		})
	}
}

func TestChangeRoundPartialQuorum(t *testing.T) {
	tests := []struct {
		name                 string
		msgs                 []*proto.SignedMessage
		expectedQuorum       bool
		expectedT, expectedN uint64
	}{
		{
			"valid f+1 quorum",
			[]*proto.SignedMessage{
				{
					Message: &proto.Message{
						Type:  proto.RoundState_ChangeRound,
						Round: 4,
					},
				},
				{
					Message: &proto.Message{
						Type:  proto.RoundState_ChangeRound,
						Round: 4,
					},
				},
			},
			true,
			2,
			4,
		},
		{
			"valid 2f+1 quorum",
			[]*proto.SignedMessage{
				{
					Message: &proto.Message{
						Type:  proto.RoundState_ChangeRound,
						Round: 4,
					},
				},
				{
					Message: &proto.Message{
						Type:  proto.RoundState_ChangeRound,
						Round: 4,
					},
				},
				{
					Message: &proto.Message{
						Type:  proto.RoundState_ChangeRound,
						Round: 4,
					},
				},
			},
			true,
			3,
			4,
		},
		{
			"valid 3f+1 quorum",
			[]*proto.SignedMessage{
				{
					Message: &proto.Message{
						Type:  proto.RoundState_ChangeRound,
						Round: 4,
					},
				},
				{
					Message: &proto.Message{
						Type:  proto.RoundState_ChangeRound,
						Round: 4,
					},
				},
				{
					Message: &proto.Message{
						Type:  proto.RoundState_ChangeRound,
						Round: 4,
					},
				},
				{
					Message: &proto.Message{
						Type:  proto.RoundState_ChangeRound,
						Round: 4,
					},
				},
			},
			true,
			4,
			4,
		},
		{
			"invalid f quorum",
			[]*proto.SignedMessage{
				{
					Message: &proto.Message{
						Type:  proto.RoundState_ChangeRound,
						Round: 4,
					},
				},
			},
			false,
			1,
			4,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(tt *testing.T) {
			instance := &Instance{
				Config: proto.DefaultConsensusParams(),
				ValidatorShare: &storage.Share{Committee: map[uint64]*proto.Node{
					0: {IbftId: 0},
					1: {IbftId: 1},
					2: {IbftId: 2},
					3: {IbftId: 3},
				}},
			}

			q, t, n := instance.changeRoundPartialQuorum(test.msgs)
			require.EqualValues(tt, test.expectedQuorum, q)
			require.EqualValues(tt, test.expectedN, n)
			require.EqualValues(tt, test.expectedT, t)
		})
	}
}
