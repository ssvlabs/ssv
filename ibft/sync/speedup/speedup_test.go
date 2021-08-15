package speedup

import (
	"github.com/bloxapp/ssv/ibft/pipeline"
	"github.com/bloxapp/ssv/ibft/proto"
	"github.com/bloxapp/ssv/ibft/sync"
	"github.com/pkg/errors"
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
			network := sync.NewTestNetwork(t, test.peers, 100, nil, nil, nil, test.changeRoundMsgs, nil)
			s := New(
				logger,
				test.identifier,
				test.validatorPk,
				network,
				pipeline.WrapFunc("", func(signedMessage *proto.SignedMessage) error {
					if signedMessage.Message.Type != proto.RoundState_ChangeRound {
						return errors.New("wrong type")
					}
					return nil
				}),
			)
			msgs, err := s.Start()

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
