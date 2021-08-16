package history

import (
	"github.com/bloxapp/ssv/ibft/proto"
	"github.com/bloxapp/ssv/ibft/sync"
	"github.com/bloxapp/ssv/storage/basedb"
	"github.com/bloxapp/ssv/storage/collections"
	"github.com/bloxapp/ssv/storage/kv"
	"github.com/pkg/errors"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"testing"
)

func TestFetchDecided(t *testing.T) {
	sks, _ := sync.GenerateNodes(4)
	tests := []struct {
		name           string
		validatorPk    []byte
		identifier     []byte
		peers          []string
		fromPeer       string
		rangeParams    []uint64
		decidedArr     map[string][]*proto.SignedMessage
		expectedError  string
		forceError     error
		expectedResLen uint64
	}{
		{
			"valid fetch no pagination",
			[]byte{1, 2, 3, 4},
			[]byte("lambda"),
			[]string{"2"},
			"2",
			[]uint64{1, 3, 3},
			map[string][]*proto.SignedMessage{
				"2": {
					sync.MultiSignMsg(t, []uint64{1, 2, 3}, sks, &proto.Message{
						Type:      proto.RoundState_Decided,
						Round:     1,
						Lambda:    []byte("lambda"),
						SeqNumber: 1,
					}),
					sync.MultiSignMsg(t, []uint64{1, 2, 3}, sks, &proto.Message{
						Type:      proto.RoundState_Decided,
						Round:     1,
						Lambda:    []byte("lambda"),
						SeqNumber: 2,
					}),
					sync.MultiSignMsg(t, []uint64{1, 2, 3}, sks, &proto.Message{
						Type:      proto.RoundState_Decided,
						Round:     1,
						Lambda:    []byte("lambda"),
						SeqNumber: 3,
					}),
				},
			},
			"",
			nil,
			3,
		},
		{
			"valid fetch with pagination",
			[]byte{1, 2, 3, 4},
			[]byte("lambda"),
			[]string{"2"},
			"2",
			[]uint64{1, 3, 2},
			map[string][]*proto.SignedMessage{
				"2": {
					sync.MultiSignMsg(t, []uint64{1, 2, 3}, sks, &proto.Message{
						Type:      proto.RoundState_Decided,
						Round:     1,
						Lambda:    []byte("lambda"),
						SeqNumber: 1,
					}),
					sync.MultiSignMsg(t, []uint64{1, 2, 3}, sks, &proto.Message{
						Type:      proto.RoundState_Decided,
						Round:     1,
						Lambda:    []byte("lambda"),
						SeqNumber: 2,
					}),
					sync.MultiSignMsg(t, []uint64{1, 2, 3}, sks, &proto.Message{
						Type:      proto.RoundState_Decided,
						Round:     1,
						Lambda:    []byte("lambda"),
						SeqNumber: 3,
					}),
				},
			},
			"",
			nil,
			3,
		},
		{
			"force error",
			[]byte{1, 2, 3, 4},
			[]byte("lambda"),
			[]string{"2"},
			"2",
			[]uint64{1, 3, 2},
			map[string][]*proto.SignedMessage{},
			"could not find highest",
			errors.New("error"),
			3,
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
			network := sync.NewTestNetwork(t, test.peers, int(test.rangeParams[2]), nil, nil, test.decidedArr, nil, nil)
			s := New(logger, test.validatorPk, test.identifier, network, &storage, func(msg *proto.SignedMessage) error {
				return nil
			})
			res, err := s.fetchValidateAndSaveInstances(test.fromPeer, test.rangeParams[0], test.rangeParams[1])

			if len(test.expectedError) > 0 {
				require.EqualError(t, err, test.expectedError)
			} else {
				require.NoError(t, err)
				require.EqualValues(t, res.Message.SeqNumber, test.expectedResLen)
			}

		})
	}
}
