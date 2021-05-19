package ibft

import (
	"github.com/bloxapp/ssv/ibft/proto"
	"github.com/bloxapp/ssv/network/local"
	"github.com/bloxapp/ssv/storage/collections"
	"github.com/bloxapp/ssv/storage/inmem"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"testing"
	"time"
)

type testStorage struct {
	highestDecided *proto.SignedMessage
}

// SaveCurrentInstance implementation
func (s *testStorage) SaveCurrentInstance(_ *proto.State) error {
	return nil
}

// GetCurrentInstance implementation
func (s *testStorage) GetCurrentInstance(_ []byte) (*proto.State, error) {
	return nil, nil
}

// SaveDecided implementation
func (s *testStorage) SaveDecided(_ *proto.SignedMessage) error {
	return nil
}

// GetDecided implementation
func (s *testStorage) GetDecided(_ []byte, _ uint64) (*proto.SignedMessage, error) {
	return nil, nil
}

// SaveHighestDecidedInstance implementation
func (s *testStorage) SaveHighestDecidedInstance(_ *proto.SignedMessage) error {
	return nil
}

// GetHighestDecidedInstance implementation
func (s *testStorage) GetHighestDecidedInstance(_ []byte) (*proto.SignedMessage, error) {
	return s.highestDecided, nil
}

func TestDecidedRequiresSync(t *testing.T) {
	secretKeys, _ := GenerateNodes(4)
	tests := []struct {
		name            string
		currentInstance *Instance
		highestDecided  *proto.SignedMessage
		msg             *proto.SignedMessage
		expectedRes     bool
		expectedErr     string
	}{
		{
			"decided from future, requires sync.",
			&Instance{
				State: &proto.State{
					Lambda:    nil,
					SeqNumber: 3,
				},
			},
			SignMsg(t, 1, secretKeys[1], &proto.Message{
				Type:      proto.RoundState_Decided,
				SeqNumber: 2,
			}),
			SignMsg(t, 1, secretKeys[1], &proto.Message{
				Type:      proto.RoundState_Decided,
				SeqNumber: 4,
			}),
			true,
			"",
		},
		{
			"decided from future, requires sync. current is nil",
			nil,
			SignMsg(t, 1, secretKeys[1], &proto.Message{
				Type:      proto.RoundState_Decided,
				SeqNumber: 2,
			}),
			SignMsg(t, 1, secretKeys[1], &proto.Message{
				Type:      proto.RoundState_Decided,
				SeqNumber: 4,
			}),
			true,
			"",
		},
		{
			"decided from far future, requires sync.",
			&Instance{
				State: &proto.State{
					Lambda:    nil,
					SeqNumber: 3,
				},
			},
			SignMsg(t, 1, secretKeys[1], &proto.Message{
				Type:      proto.RoundState_Decided,
				SeqNumber: 2,
			}),
			SignMsg(t, 1, secretKeys[1], &proto.Message{
				Type:      proto.RoundState_Decided,
				SeqNumber: 10,
			}),
			true,
			"",
		},
		{
			"decided from past, doesn't requires sync.",
			&Instance{
				State: &proto.State{
					Lambda:    nil,
					SeqNumber: 3,
				},
			},
			SignMsg(t, 1, secretKeys[1], &proto.Message{
				Type:      proto.RoundState_Decided,
				SeqNumber: 2,
			}),
			SignMsg(t, 1, secretKeys[1], &proto.Message{
				Type:      proto.RoundState_Decided,
				SeqNumber: 1,
			}),
			false,
			"",
		},
		{
			"decided for current",
			&Instance{
				State: &proto.State{
					Lambda:    nil,
					SeqNumber: 3,
				},
			},
			SignMsg(t, 1, secretKeys[1], &proto.Message{
				Type:      proto.RoundState_Decided,
				SeqNumber: 2,
			}),
			SignMsg(t, 1, secretKeys[1], &proto.Message{
				Type:      proto.RoundState_Decided,
				SeqNumber: 3,
			}),
			false,
			"",
		},
		{
			"decided for seq 0",
			&Instance{
				State: &proto.State{
					Lambda:    nil,
					SeqNumber: 0,
				},
			},
			nil,
			SignMsg(t, 1, secretKeys[1], &proto.Message{
				Type:      proto.RoundState_Decided,
				SeqNumber: 0,
			}),
			false,
			"",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			ibft := ibftImpl{
				currentInstance: test.currentInstance,
				ibftStorage:     &testStorage{highestDecided: test.highestDecided},
			}
			res, err := ibft.decidedRequiresSync(test.msg)
			require.EqualValues(t, test.expectedRes, res)
			if len(test.expectedErr) > 0 {
				require.EqualError(t, err, test.expectedErr)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func TestDecideIsCurrentInstance(t *testing.T) {
	secretKeys, _ := GenerateNodes(4)
	tests := []struct {
		name            string
		currentInstance *Instance
		msg             *proto.SignedMessage
		expectedRes     bool
	}{
		{
			"current instance",
			&Instance{
				State: &proto.State{
					Lambda:    nil,
					SeqNumber: 1,
				},
			},
			SignMsg(t, 1, secretKeys[1], &proto.Message{
				Type:      proto.RoundState_Decided,
				SeqNumber: 1,
			}),
			true,
		},
		{
			"current instance nil",
			nil,
			SignMsg(t, 1, secretKeys[1], &proto.Message{
				Type:      proto.RoundState_Decided,
				SeqNumber: 1,
			}),
			false,
		},
		{
			"current instance seq lower",
			&Instance{
				State: &proto.State{
					Lambda:    nil,
					SeqNumber: 1,
				},
			},
			SignMsg(t, 1, secretKeys[1], &proto.Message{
				Type:      proto.RoundState_Decided,
				SeqNumber: 2,
			}),
			false,
		},
		{
			"current instance seq higher",
			&Instance{
				State: &proto.State{
					Lambda:    nil,
					SeqNumber: 4,
				},
			},
			SignMsg(t, 1, secretKeys[1], &proto.Message{
				Type:      proto.RoundState_Decided,
				SeqNumber: 2,
			}),
			false,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			ibft := ibftImpl{currentInstance: test.currentInstance}
			require.EqualValues(t, test.expectedRes, ibft.decidedForCurrentInstance(test.msg))
		})
	}
}

func TestSyncAfterDecided(t *testing.T) {
	sks, nodes := GenerateNodes(4)
	network := local.NewLocalNetwork()

	s1 := populatedStorage(t, sks, 4)
	i1 := populatedIbft(1, network, s1, sks, nodes)

	_ = populatedIbft(2, network, populatedStorage(t, sks, 10), sks, nodes)

	// test before sync
	highest, err := i1.(*ibftImpl).ibftStorage.GetHighestDecidedInstance(validatorPK(sks).Serialize())
	require.NoError(t, err)
	require.EqualValues(t, 4, highest.Message.SeqNumber)

	decidedMsg := aggregateSign(t, sks, &proto.Message{
		Type:        proto.RoundState_Decided,
		Round:       3,
		SeqNumber:   10,
		ValidatorPk: validatorPK(sks).Serialize(),
		Lambda:      []byte("Lambda"),
		Value:       []byte("value"),
	})

	i1.(*ibftImpl).ProcessDecidedMessage(decidedMsg)

	time.Sleep(time.Millisecond * 500) // wait for sync to complete
	highest, err = i1.(*ibftImpl).ibftStorage.GetHighestDecidedInstance(validatorPK(sks).Serialize())
	require.NoError(t, err)
	require.EqualValues(t, 10, highest.Message.SeqNumber)
}

func TestSyncFromScratchAfterDecided(t *testing.T) {
	sks, nodes := GenerateNodes(4)
	network := local.NewLocalNetwork()

	s1 := collections.NewIbft(inmem.New(), zap.L(), "attestation")
	i1 := populatedIbft(1, network, &s1, sks, nodes)

	_ = populatedIbft(2, network, populatedStorage(t, sks, 10), sks, nodes)

	decidedMsg := aggregateSign(t, sks, &proto.Message{
		Type:        proto.RoundState_Decided,
		Round:       3,
		SeqNumber:   10,
		ValidatorPk: validatorPK(sks).Serialize(),
		Lambda:      []byte("Lambda"),
		Value:       []byte("value"),
	})

	i1.(*ibftImpl).ProcessDecidedMessage(decidedMsg)

	time.Sleep(time.Millisecond * 500) // wait for sync to complete
	highest, err := i1.(*ibftImpl).ibftStorage.GetHighestDecidedInstance(validatorPK(sks).Serialize())
	require.NoError(t, err)
	require.EqualValues(t, 10, highest.Message.SeqNumber)
}
