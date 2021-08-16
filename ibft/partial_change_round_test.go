package ibft

import (
	"github.com/bloxapp/ssv/ibft/proto"
	"github.com/bloxapp/ssv/network"
	"github.com/bloxapp/ssv/utils/dataval/bytesval"
	"github.com/bloxapp/ssv/utils/threadsafe"
	"github.com/bloxapp/ssv/validator/storage"
	"github.com/herumi/bls-eth-go-binary/bls"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"testing"
)

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

func TestFindPartialChangeRound(t *testing.T) {
	sks, nodes := GenerateNodes(4)

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
			instance := &Instance{
				State:          &proto.State{Round: 1, Lambda: threadsafe.Bytes([]byte{1, 2, 3, 4}), SeqNumber: threadsafe.Uint64(1)},
				Config:         proto.DefaultConsensusParams(),
				ValidatorShare: &storage.Share{Committee: nodes},
				Logger:         zap.L(),
				ValueCheck:     bytesval.New([]byte{1, 2, 3, 4}),
			}

			found, lowest := instance.findPartialQuorum(test.msgs)
			require.EqualValues(tt, test.expectedFound, found)
			require.EqualValues(tt, test.expectedLowest, lowest)
		})
	}
}
