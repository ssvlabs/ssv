package inmem

import (
	"encoding/json"
	"testing"

	"github.com/herumi/bls-eth-go-binary/bls"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	"github.com/bloxapp/ssv/ibft/proto"
	"github.com/bloxapp/ssv/network"
	v0 "github.com/bloxapp/ssv/network/forks/v0"
	"github.com/bloxapp/ssv/protocol/v1/message"
	"github.com/bloxapp/ssv/utils/logex"
)

func init() {
	logex.Build("test", zap.InfoLevel, nil)
}

func changeRoundDataToBytes(input *proto.ChangeRoundData) []byte {
	ret, _ := json.Marshal(input)
	return ret
}

func signedMsgToNetworkMsg(t *testing.T, id uint64, sk *bls.SecretKey, round uint64) *network.Message {
	return &network.Message{
		SignedMessage: SignMsg(t, id, sk, &proto.Message{
			Type:      proto.RoundState_ChangeRound,
			Round:     round,
			Lambda:    []byte{1, 2, 3, 4},
			SeqNumber: 1,
			Value:     changeRoundDataToBytes(&proto.ChangeRoundData{}),
		}),
	}
}

// SignMsg signs the given message by the given private key
func SignMsg(t *testing.T, id uint64, sk *bls.SecretKey, msg *proto.Message) *proto.SignedMessage {
	bls.Init(bls.BLS12_381)

	signature, err := msg.Sign(sk)
	require.NoError(t, err)
	return &proto.SignedMessage{
		Message:   msg,
		Signature: signature.Serialize(),
		SignerIds: []uint64{id},
	}
}

// GenerateNodes generates randomly nodes
func GenerateNodes(cnt int) (map[uint64]*bls.SecretKey, map[uint64]*proto.Node) {
	_ = bls.Init(bls.BLS12_381)
	nodes := make(map[uint64]*proto.Node)
	sks := make(map[uint64]*bls.SecretKey)
	for i := 1; i <= cnt; i++ {
		sk := &bls.SecretKey{}
		sk.SetByCSPRNG()

		nodes[uint64(i)] = &proto.Node{
			IbftId: uint64(i),
			Pk:     sk.GetPublicKey().Serialize(),
		}
		sks[uint64(i)] = sk
	}
	return sks, nodes
}

func TestFindPartialChangeRound(t *testing.T) {
	sks, _ := GenerateNodes(4)

	tests := []struct {
		name           string
		msgs           []*network.Message
		expectedFound  bool
		expectedLowest uint64
	}{
		{
			"lowest 4",
			[]*network.Message{
				signedMsgToNetworkMsg(t, 1, sks[1], 4),
				signedMsgToNetworkMsg(t, 2, sks[2], 7),
			},
			true,
			4,
		},
		{
			"lowest is lower than state round",
			[]*network.Message{
				signedMsgToNetworkMsg(t, 1, sks[1], 1),
				signedMsgToNetworkMsg(t, 2, sks[2], 0),
			},
			false,
			100000,
		},
		{
			"lowest 7",
			[]*network.Message{
				signedMsgToNetworkMsg(t, 1, sks[1], 7),
				signedMsgToNetworkMsg(t, 2, sks[2], 9),
				signedMsgToNetworkMsg(t, 3, sks[3], 10),
			},
			true,
			7,
		},
		{
			"not found",
			[]*network.Message{},
			false,
			100000,
		},
		{
			"duplicate msgs from same peer, no quorum",
			[]*network.Message{
				signedMsgToNetworkMsg(t, 1, sks[1], 4),
				signedMsgToNetworkMsg(t, 1, sks[1], 5),
			},
			false,
			4,
		},
		{
			"duplicate msgs from same peer, lowest 8",
			[]*network.Message{
				signedMsgToNetworkMsg(t, 1, sks[1], 13),
				signedMsgToNetworkMsg(t, 1, sks[1], 12),
				signedMsgToNetworkMsg(t, 2, sks[2], 10),
				signedMsgToNetworkMsg(t, 2, sks[2], 8),
			},
			true,
			8,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(tt *testing.T) {
			c := New(3, 2)
			for _, msg := range test.msgs {
				v1, err := v0.ToSignedMessageV1(msg.SignedMessage)
				require.NoError(tt, err)
				c.AddMessage(v1, nil)
			}

			found, lowest := c.PartialChangeRoundQuorum(1)
			require.EqualValues(tt, test.expectedFound, found)
			require.EqualValues(tt, test.expectedLowest, lowest)
		})
	}
}

func TestMessagesContainer_OverrideMessages(t *testing.T) {
	c := New(3, 2)
	c.AddMessage(&message.SignedMessage{
		Message: &message.ConsensusMessage{
			Round:      1,
			Identifier: nil,
			Data:       []byte{1, 1, 1, 1},
		},
		Signature: nil,
		Signers:   []message.OperatorID{1, 4},
	}, nil)
	c.AddMessage(&message.SignedMessage{
		Message: &message.ConsensusMessage{
			Round:      1,
			Identifier: nil,
			Data:       []byte{1, 1, 1, 1},
		},
		Signature: nil,
		Signers:   []message.OperatorID{2, 3},
	}, nil)

	c.OverrideMessages(&message.SignedMessage{
		Message: &message.ConsensusMessage{
			Round:      1,
			Identifier: nil,
			Data:       []byte{1, 1, 1, 1},
		},
		Signature: nil,
		Signers:   []message.OperatorID{1},
	}, nil)
	require.Len(t, c.ReadOnlyMessagesByRound(1), 1)
}

func TestMessagesContainer_AddMessage(t *testing.T) {
	c := New(3, 2)
	c.AddMessage(&message.SignedMessage{
		Message: &message.ConsensusMessage{
			Round:      1,
			Identifier: nil,
			Data:       []byte{1, 1, 1, 1},
		},
		Signature: nil,
		Signers:   []message.OperatorID{1, 2, 3, 4},
	}, nil)

	require.Len(t, c.ReadOnlyMessagesByRound(1), 1)
	require.Len(t, c.ReadOnlyMessagesByRound(2), 0)

	// try to add duplicate
	c.AddMessage(&message.SignedMessage{
		Message: &message.ConsensusMessage{
			Round:      1,
			Identifier: nil,
			Data:       []byte{1, 1, 1, 1},
		},
		Signature: nil,
		Signers:   []message.OperatorID{4, 5},
	}, nil)
	require.Len(t, c.ReadOnlyMessagesByRound(1), 1)
	require.Len(t, c.ReadOnlyMessagesByRound(2), 0)
	c.AddMessage(&message.SignedMessage{
		Message: &message.ConsensusMessage{
			Round:      1,
			Identifier: nil,
			Data:       []byte{1, 1, 1, 1},
		},
		Signature: nil,
		Signers:   []message.OperatorID{4},
	}, nil)
	require.Len(t, c.ReadOnlyMessagesByRound(1), 1)
	require.Len(t, c.ReadOnlyMessagesByRound(2), 0)
}

func TestMessagesContainer_ReadOnlyMessagesByRound(t *testing.T) {
	c := New(3, 2)
	c.AddMessage(&message.SignedMessage{
		Message: &message.ConsensusMessage{
			Round:      1,
			Identifier: nil,
			Data:       []byte{1, 1, 1, 1},
		},
		Signature: nil,
		Signers:   []message.OperatorID{1, 2, 3, 4},
	}, nil)
	c.AddMessage(&message.SignedMessage{
		Message: &message.ConsensusMessage{
			Round:      1,
			Identifier: nil,
			Data:       []byte{1, 1, 1, 1},
		},
		Signature: nil,
		Signers:   []message.OperatorID{5},
	}, nil)

	msgs := c.ReadOnlyMessagesByRound(1)
	require.EqualValues(t, 1, msgs[0].Message.Round)
	require.EqualValues(t, 1, msgs[1].Message.Round)
	require.EqualValues(t, []byte{1, 1, 1, 1}, msgs[0].Message.Data)
	require.EqualValues(t, []byte{1, 1, 1, 1}, msgs[1].Message.Data)
	require.EqualValues(t, []message.OperatorID{1, 2, 3, 4}, msgs[0].Signers)
	require.EqualValues(t, []message.OperatorID{5}, msgs[1].Signers)
}

func TestMessagesContainer_QuorumAchieved(t *testing.T) {
	c := New(3, 2)
	c.AddMessage(&message.SignedMessage{
		Message: &message.ConsensusMessage{
			Round:      1,
			Identifier: nil,
			Data:       []byte{1, 1, 1, 1},
		},
		Signature: nil,
		Signers:   []message.OperatorID{1, 2, 3},
	}, []byte{1, 1, 1, 1})
	res, _ := c.QuorumAchieved(1, []byte{1, 1, 1, 1})
	require.True(t, res)
	res, _ = c.QuorumAchieved(0, []byte{1, 1, 1, 1})
	require.False(t, res)
	res, _ = c.QuorumAchieved(1, []byte{1, 1, 1, 0})
	require.False(t, res)

	c.AddMessage(&message.SignedMessage{
		Message: &message.ConsensusMessage{
			Round:      2,
			Identifier: nil,
			Data:       []byte{1, 1, 1, 1},
		},
		Signature: nil,
		Signers:   []message.OperatorID{1, 2},
	}, []byte{1, 1, 1, 1})
	res, _ = c.QuorumAchieved(2, []byte{1, 1, 1, 1})
	require.False(t, res)
	c.AddMessage(&message.SignedMessage{
		Message: &message.ConsensusMessage{
			Round:      2,
			Identifier: nil,
			Data:       []byte{1, 1, 1, 1},
		},
		Signature: nil,
		Signers:   []message.OperatorID{3},
	}, []byte{1, 1, 1, 1})
	res, _ = c.QuorumAchieved(2, []byte{1, 1, 1, 1})
	require.True(t, res)
	res, _ = c.QuorumAchieved(3, []byte{1, 1, 1, 1})
	require.False(t, res)
	res, _ = c.QuorumAchieved(2, []byte{1, 1, 1, 0})
	require.False(t, res)
}
