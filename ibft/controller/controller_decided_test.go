package controller

import (
	"github.com/bloxapp/ssv/beacon/valcheck"
	"github.com/bloxapp/ssv/ibft"
	instance "github.com/bloxapp/ssv/ibft/instance"
	"github.com/bloxapp/ssv/ibft/proto"
	"github.com/bloxapp/ssv/network/local"
	"github.com/bloxapp/ssv/storage/basedb"
	"github.com/bloxapp/ssv/storage/collections"
	"github.com/bloxapp/ssv/storage/kv"
	"github.com/bloxapp/ssv/utils/logex"
	"github.com/bloxapp/ssv/utils/threadsafe"
	"github.com/bloxapp/ssv/validator/storage"
	"github.com/pkg/errors"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"testing"
	"time"
)

type testStorage struct {
	highestDecided *proto.SignedMessage
}

// SaveCurrentInstance implementation
func (s *testStorage) SaveCurrentInstance(identifier []byte, state *proto.State) error {
	return nil
}

// GetCurrentInstance implementation
func (s *testStorage) GetCurrentInstance(identifier []byte) (*proto.State, bool, error) {
	return nil, false, nil
}

// SaveDecided implementation
func (s *testStorage) SaveDecided(_ *proto.SignedMessage) error {
	return nil
}

// GetDecided implementation
func (s *testStorage) GetDecided(identifier []byte, seqNumber uint64) (*proto.SignedMessage, bool, error) {
	return nil, false, nil
}

// SaveHighestDecidedInstance implementation
func (s *testStorage) SaveHighestDecidedInstance(_ *proto.SignedMessage) error {
	return nil
}

// GetHighestDecidedInstance implementation
func (s *testStorage) GetHighestDecidedInstance(identifier []byte) (*proto.SignedMessage, bool, error) {
	return s.highestDecided, true, nil
}

func TestDecidedRequiresSync(t *testing.T) {
	secretKeys, _ := GenerateNodes(4)
	tests := []struct {
		name            string
		currentInstance ibft.Instance
		highestDecided  *proto.SignedMessage
		msg             *proto.SignedMessage
		expectedRes     bool
		expectedErr     string
	}{
		{
			"decided from future, requires sync.",
			instance.NewInstanceWithState(&proto.State{
				Lambda:    nil,
				SeqNumber: threadsafe.Uint64(3),
			}),
			SignMsg(t, 1, secretKeys[1], &proto.Message{
				Type:      proto.RoundState_Commit,
				SeqNumber: 2,
			}),
			SignMsg(t, 1, secretKeys[1], &proto.Message{
				Type:      proto.RoundState_Commit,
				SeqNumber: 4,
			}),
			true,
			"",
		},
		{
			"decided from future, requires sync. current is nil",
			nil,
			SignMsg(t, 1, secretKeys[1], &proto.Message{
				Type:      proto.RoundState_Commit,
				SeqNumber: 2,
			}),
			SignMsg(t, 1, secretKeys[1], &proto.Message{
				Type:      proto.RoundState_Commit,
				SeqNumber: 4,
			}),
			true,
			"",
		},
		{
			"decided from far future, requires sync.",
			instance.NewInstanceWithState(&proto.State{
				Lambda:    nil,
				SeqNumber: threadsafe.Uint64(3),
			}),
			SignMsg(t, 1, secretKeys[1], &proto.Message{
				Type:      proto.RoundState_Commit,
				SeqNumber: 2,
			}),
			SignMsg(t, 1, secretKeys[1], &proto.Message{
				Type:      proto.RoundState_Commit,
				SeqNumber: 10,
			}),
			true,
			"",
		},
		{
			"decided from past, doesn't requires sync.",
			instance.NewInstanceWithState(&proto.State{
				Lambda:    nil,
				SeqNumber: threadsafe.Uint64(3),
			}),
			SignMsg(t, 1, secretKeys[1], &proto.Message{
				Type:      proto.RoundState_Commit,
				SeqNumber: 2,
			}),
			SignMsg(t, 1, secretKeys[1], &proto.Message{
				Type:      proto.RoundState_Commit,
				SeqNumber: 1,
			}),
			false,
			"",
		},
		{
			"decided for current",
			instance.NewInstanceWithState(&proto.State{
				Lambda:    nil,
				SeqNumber: threadsafe.Uint64(3),
			}),
			SignMsg(t, 1, secretKeys[1], &proto.Message{
				Type:      proto.RoundState_Commit,
				SeqNumber: 2,
			}),
			SignMsg(t, 1, secretKeys[1], &proto.Message{
				Type:      proto.RoundState_Commit,
				SeqNumber: 3,
			}),
			false,
			"",
		},
		{
			"decided for seq 0",
			instance.NewInstanceWithState(&proto.State{
				Lambda:    nil,
				SeqNumber: threadsafe.Uint64(0),
			}),
			nil,
			SignMsg(t, 1, secretKeys[1], &proto.Message{
				Type:      proto.RoundState_Commit,
				SeqNumber: 0,
			}),
			false,
			"",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			ibft := Controller{
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
		currentInstance ibft.Instance
		msg             *proto.SignedMessage
		expectedRes     bool
	}{
		{
			"current instance",
			instance.NewInstanceWithState(&proto.State{
				Lambda:    nil,
				SeqNumber: threadsafe.Uint64(1),
			}),
			SignMsg(t, 1, secretKeys[1], &proto.Message{
				Type:      proto.RoundState_Commit,
				SeqNumber: 1,
			}),
			true,
		},
		{
			"current instance nil",
			nil,
			SignMsg(t, 1, secretKeys[1], &proto.Message{
				Type:      proto.RoundState_Commit,
				SeqNumber: 1,
			}),
			false,
		},
		{
			"current instance seq lower",
			instance.NewInstanceWithState(&proto.State{
				Lambda:    nil,
				SeqNumber: threadsafe.Uint64(1),
			}),
			SignMsg(t, 1, secretKeys[1], &proto.Message{
				Type:      proto.RoundState_Commit,
				SeqNumber: 2,
			}),
			false,
		},
		{
			"current instance seq higher",
			instance.NewInstanceWithState(&proto.State{
				Lambda:    nil,
				SeqNumber: threadsafe.Uint64(4),
			}),
			SignMsg(t, 1, secretKeys[1], &proto.Message{
				Type:      proto.RoundState_Commit,
				SeqNumber: 2,
			}),
			false,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			ibft := Controller{currentInstance: test.currentInstance}
			require.EqualValues(t, test.expectedRes, ibft.decidedForCurrentInstance(test.msg))
		})
	}
}

func TestForceDecided(t *testing.T) {
	sks, nodes := GenerateNodes(4)
	network := local.NewLocalNetwork()

	identifier := []byte("lambda_11")
	s1 := populatedStorage(t, sks, 3)
	i1 := populatedIbft(1, identifier, network, s1, sks, nodes, newTestSigner())

	// test before sync
	highest, found, err := i1.(*Controller).ibftStorage.GetHighestDecidedInstance(identifier)
	require.True(t, found)
	require.NoError(t, err)
	require.EqualValues(t, 3, highest.Message.SeqNumber)

	time.Sleep(time.Second * 1) // wait for sync to complete

	go func() {
		time.Sleep(time.Millisecond * 500) // wait for instance to start
		decidedMsg := aggregateSign(t, sks, &proto.Message{
			Type:      proto.RoundState_Commit,
			Round:     1,
			SeqNumber: 4,
			Lambda:    identifier,
			Value:     []byte("value"),
		})
		i1.(*Controller).ProcessDecidedMessage(decidedMsg)
	}()

	share := &storage.Share{
		NodeID:    1,
		PublicKey: validatorPK(sks),
		Committee: nodes,
	}
	res, err := i1.StartInstance(ibft.ControllerStartInstanceOptions{
		Logger:         logex.GetLogger(),
		ValueCheck:     &valcheck.AttestationValueCheck{},
		SeqNumber:      4,
		Value:          []byte("value"),
		ValidatorShare: share,
	})
	require.NoError(t, err)
	require.True(t, res.Decided)

	highest, found, err = i1.(*Controller).ibftStorage.GetHighestDecidedInstance(identifier)
	require.True(t, found)
	require.NoError(t, err)
	require.EqualValues(t, 4, highest.Message.SeqNumber)
}

func TestSyncAfterDecided(t *testing.T) {
	sks, nodes := GenerateNodes(4)
	network := local.NewLocalNetwork()

	identifier := []byte("lambda_11")
	s1 := populatedStorage(t, sks, 4)
	i1 := populatedIbft(1, identifier, network, s1, sks, nodes, newTestSigner())

	_ = populatedIbft(2, identifier, network, populatedStorage(t, sks, 10), sks, nodes, newTestSigner())

	// test before sync
	highest, found, err := i1.(*Controller).ibftStorage.GetHighestDecidedInstance(identifier)
	require.True(t, found)
	require.NoError(t, err)
	require.EqualValues(t, 4, highest.Message.SeqNumber)

	decidedMsg := aggregateSign(t, sks, &proto.Message{
		Type:      proto.RoundState_Commit,
		Round:     3,
		SeqNumber: 10,
		Lambda:    identifier,
		Value:     []byte("value"),
	})

	i1.(*Controller).ProcessDecidedMessage(decidedMsg)

	time.Sleep(time.Millisecond * 500) // wait for sync to complete
	highest, found, err = i1.(*Controller).ibftStorage.GetHighestDecidedInstance(identifier)
	require.True(t, found)
	require.NoError(t, err)
	require.EqualValues(t, 10, highest.Message.SeqNumber)
}

func TestSyncFromScratchAfterDecided(t *testing.T) {
	sks, nodes := GenerateNodes(4)
	network := local.NewLocalNetwork()
	db, _ := kv.New(basedb.Options{
		Type:   "badger-memory",
		Path:   "",
		Logger: zap.L(),
	})

	identifier := []byte("lambda_11")
	s1 := collections.NewIbft(db, zap.L(), "attestation")
	i1 := populatedIbft(1, identifier, network, &s1, sks, nodes, newTestSigner())

	_ = populatedIbft(2, identifier, network, populatedStorage(t, sks, 10), sks, nodes, newTestSigner())

	decidedMsg := aggregateSign(t, sks, &proto.Message{
		Type:      proto.RoundState_Commit,
		Round:     3,
		SeqNumber: 10,
		Lambda:    identifier,
		Value:     []byte("value"),
	})

	i1.(*Controller).ProcessDecidedMessage(decidedMsg)

	time.Sleep(time.Millisecond * 500) // wait for sync to complete
	highest, found, err := i1.(*Controller).ibftStorage.GetHighestDecidedInstance(identifier)
	require.True(t, found)
	require.NoError(t, err)
	require.EqualValues(t, 10, highest.Message.SeqNumber)
}

func TestValidateDecidedMsg(t *testing.T) {
	sks, nodes := GenerateNodes(4)
	network := local.NewLocalNetwork()
	identifier := []byte("lambda_11")
	ibft := populatedIbft(1, identifier, network, populatedStorage(t, sks, 10), sks, nodes, newTestSigner())

	tests := []struct {
		name          string
		msg           *proto.SignedMessage
		expectedError error
	}{
		{
			"valid",
			aggregateSign(t, sks, &proto.Message{
				Type:      proto.RoundState_Commit,
				Round:     3,
				SeqNumber: 11,
				Lambda:    identifier,
				Value:     []byte("value"),
			}),
			nil,
		},
		{
			"invalid msg stage",
			aggregateSign(t, sks, &proto.Message{
				Type:      proto.RoundState_Prepare,
				Round:     3,
				SeqNumber: 11,
				Lambda:    identifier,
				Value:     []byte("value"),
			}),
			errors.New("message type is wrong"),
		},
		{
			"invalid msg sig",
			aggregateInvalidSign(t, sks, &proto.Message{
				Type:      proto.RoundState_Commit,
				Round:     3,
				SeqNumber: 11,
				Lambda:    identifier,
				Value:     []byte("value"),
			}),
			errors.New("could not verify message signature"),
		},
		{
			"valid first decided",
			aggregateSign(t, sks, &proto.Message{
				Type:      proto.RoundState_Commit,
				Round:     3,
				SeqNumber: 0,
				Lambda:    identifier,
				Value:     []byte("value"),
			}),
			nil,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if test.expectedError != nil {
				err := ibft.(*Controller).ValidateDecidedMsg(test.msg)
				require.EqualError(t, err, test.expectedError.Error())
			} else {
				require.NoError(t, ibft.(*Controller).ValidateDecidedMsg(test.msg))
			}
		})
	}
}
