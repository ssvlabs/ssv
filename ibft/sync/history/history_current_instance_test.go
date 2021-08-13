package history

import (
	"errors"
	"github.com/bloxapp/ssv/ibft/proto"
	"github.com/bloxapp/ssv/ibft/sync"
	"github.com/bloxapp/ssv/storage/basedb"
	"github.com/bloxapp/ssv/storage/collections"
	"github.com/bloxapp/ssv/storage/kv"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"testing"
)

func TestFetchPeersLastChangeRoundMsg(t *testing.T) {
	sks, _ := sync.GenerateNodes(4)
	change1 := sync.MultiSignMsg(t, []uint64{1}, sks, &proto.Message{
		Type:      proto.RoundState_ChangeRound,
		Round:     1,
		Lambda:    []byte("lambda"),
		SeqNumber: 0,
	})
	invalidType1 := sync.MultiSignMsg(t, []uint64{1}, sks, &proto.Message{
		Type:      proto.RoundState_Decided,
		Round:     1,
		Lambda:    []byte("lambda"),
		SeqNumber: 0,
	})

	tests := []struct {
		name            string
		validatorPk     []byte
		identifier      []byte
		peers           []string
		changeRoundMsgs map[string]*proto.SignedMessage
		expectedMsgs    []*proto.SignedMessage
		expectedError   string
	}{
		{
			"valid msg",
			[]byte{1, 2, 3, 4},
			[]byte("lambda"),
			[]string{"2"},
			map[string]*proto.SignedMessage{
				"2": change1,
				"3": nil,
			},
			[]*proto.SignedMessage{change1},
			"",
		},
		{
			"valid msgs",
			[]byte{1, 2, 3, 4},
			[]byte("lambda"),
			[]string{"2", "3"},
			map[string]*proto.SignedMessage{
				"2": change1,
				"3": change1,
			},
			[]*proto.SignedMessage{change1, change1},
			"",
		},
		{
			"invalid msg",
			[]byte{1, 2, 3, 4},
			[]byte("lambda"),
			[]string{"2"},
			map[string]*proto.SignedMessage{
				"2": invalidType1,
				"3": nil,
			},
			[]*proto.SignedMessage{},
			"",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			logger := zap.L()
			db, err := kv.New(basedb.Options{
				Type:   "badger-memory",
				Path:   "",
				Logger: logger,
			})
			require.NoError(t, err)
			storage := collections.NewIbft(db, logger, "attestation")
			network := sync.NewTestNetwork(t, test.peers, 100, nil, nil, nil, test.changeRoundMsgs, nil)
			s := New(logger, test.validatorPk, test.identifier, network, &storage, nil, func(msg *proto.SignedMessage) error {
				if msg.Message.Type != proto.RoundState_ChangeRound {
					return errors.New("wrong type")
				}
				return nil
			})
			msgs, err := s.getPeersLastChangeRoundMsgs()

			if len(test.expectedError) > 0 {
				require.EqualError(t, err, test.expectedError)
			} else {
				require.Len(t, msgs, len(test.expectedMsgs))
				for i, m := range test.expectedMsgs {
					require.EqualValues(t, m.Message.Type, msgs[i].Message.Type)
					require.EqualValues(t, m.Message.SeqNumber, msgs[i].Message.SeqNumber)
				}
			}
		})
	}
}
