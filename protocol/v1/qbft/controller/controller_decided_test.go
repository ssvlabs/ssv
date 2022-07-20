package controller

import (
	"context"
	"crypto/rsa"
	"testing"
	"time"

	"github.com/attestantio/go-eth2-client/spec/altair"
	spec "github.com/attestantio/go-eth2-client/spec/phase0"
	specqbft "github.com/bloxapp/ssv-spec/qbft"
	spectypes "github.com/bloxapp/ssv-spec/types"
	"github.com/herumi/bls-eth-go-binary/bls"
	"github.com/pkg/errors"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	forksprotocol "github.com/bloxapp/ssv/protocol/forks"
	beaconprotocol "github.com/bloxapp/ssv/protocol/v1/blockchain/beacon"
	"github.com/bloxapp/ssv/protocol/v1/message"
	protocolp2p "github.com/bloxapp/ssv/protocol/v1/p2p"
	"github.com/bloxapp/ssv/protocol/v1/qbft"
	"github.com/bloxapp/ssv/protocol/v1/qbft/instance"
	qbftstorage "github.com/bloxapp/ssv/protocol/v1/qbft/storage"
	testingprotocol "github.com/bloxapp/ssv/protocol/v1/testing"
	"github.com/bloxapp/ssv/storage/basedb"
	"github.com/bloxapp/ssv/storage/kv"
)

//
//type testStorage struct {
//	highestDecided *specqbft.SignedMessage
//	msgs           map[string]*specqbft.SignedMessage
//	lock           sync.Mutex
//}
//
//func newTestStorage(highestDecided *specqbft.SignedMessage) qbftstorage.QBFTStore {
//	return &testStorage{
//		highestDecided: highestDecided,
//		msgs:           map[string]*specqbft.SignedMessage{},
//		lock:           sync.Mutex{},
//	}
//}
//
//func msgKey(identifier []byte, Height specqbft.Height) string {
//	return fmt.Sprintf("%s_%d", string(identifier), Height)
//}
//
//func (s *testStorage) GetLastDecided(identifier message.Identifier) (*specqbft.SignedMessage, error) {
//	return s.highestDecided, nil
//}
//
//// SaveLastDecided saves the given decided message, after checking that it is indeed the highest
//func (s *testStorage) SaveLastDecided(signedMsg ...*specqbft.SignedMessage) error {
//	return nil
//}
//
//// GetDecided returns historical decided messages in the given range
//func (s *testStorage) GetDecided(identifier message.Identifier, from specqbft.Height, to specqbft.Height) ([]*specqbft.SignedMessage, error) {
//	s.lock.Lock()
//	defer s.lock.Unlock()
//
//	var msgs []*specqbft.SignedMessage
//	for i := from; i <= to; i++ {
//		k := msgKey(identifier, i)
//		if msg, ok := s.msgs[k]; ok {
//			msgs = append(msgs, msg)
//		}
//	}
//	return msgs, nil
//}
//
//// SaveDecided saves historical decided messages
//func (s *testStorage) SaveDecided(signedMsg ...*specqbft.SignedMessage) error {
//	s.lock.Lock()
//	defer s.lock.Unlock()
//
//	for _, msg := range signedMsg {
//		if msg == nil || msg.Message == nil {
//			continue
//		}
//		k := msgKey(msg.Message.Identifier, msg.Message.Height)
//		s.msgs[k] = msg
//	}
//	return nil
//}
//
//// SaveCurrentInstance saves the state for the current running (not yet decided) instance
//func (s *testStorage) SaveCurrentInstance(identifier message.Identifier, state *qbft.State) error {
//	return nil
//}
//
//// GetCurrentInstance returns the state for the current running (not yet decided) instance
//func (s *testStorage) GetCurrentInstance(identifier message.Identifier) (*qbft.State, bool, error) {
//	return nil, false, nil
//}
//
//// GetLastChangeRoundMsg returns the latest broadcasted msg from the instance
//func (s *testStorage) GetLastChangeRoundMsg(identifier message.Identifier) (*specqbft.SignedMessage, error) {
//	return nil, nil
//}
//
//func (s *testStorage) SaveLastChangeRoundMsg(msg *specqbft.SignedMessage) error {
//	return nil
//}
//
//func (s *testStorage) CleanLastChangeRound(identifier message.Identifier) {}

//func TestDecidedRequiresSync(t *testing.T) {
//	uids := []spectypes.OperatorID{spectypes.OperatorID(1), spectypes.OperatorID(2), spectypes.OperatorID(3), spectypes.OperatorID(4)}
//	secretKeys, _ := testingprotocol.GenerateBLSKeys(uids...)
//
//	height0 := atomic.Value{}
//	height0.Store(specqbft.Height(0))
//
//	height3 := atomic.Value{}
//	height3.Store(specqbft.Height(3))
//
//	tests := []struct {
//		name            string
//		currentInstance instance.Instancer
//		highestDecided  *specqbft.SignedMessage
//		msg             *specqbft.SignedMessage
//		expectedRes     bool
//		expectedErr     string
//		initState       uint32
//	}{
//		{
//			"decided from future, requires sync.",
//			instance.NewInstanceWithState(&qbft.State{
//				Height: height3,
//			}),
//			testingprotocol.SignMsg(t, secretKeys, []spectypes.OperatorID{spectypes.OperatorID(1)}, &specqbft.Message{
//				MsgType: message.CommitMsgType,
//				Height:  2,
//				Data:    commitDataToBytes(t, &specqbft.CommitData{Data: []byte("value")}),
//			}),
//			testingprotocol.SignMsg(t, secretKeys, []spectypes.OperatorID{spectypes.OperatorID(1)}, &specqbft.Message{
//				MsgType: message.CommitMsgType,
//				Height:  4,
//				Data:    commitDataToBytes(t, &specqbft.CommitData{Data: []byte("value")}),
//			}),
//			true,
//			"",
//			Ready,
//		},
//		{
//			"decided from future, requires sync. current is nil",
//			nil,
//			testingprotocol.SignMsg(t, secretKeys, []spectypes.OperatorID{spectypes.OperatorID(1)}, &specqbft.Message{
//				MsgType: message.CommitMsgType,
//				Height:  2,
//				Data:    commitDataToBytes(t, &specqbft.CommitData{Data: []byte("value")}),
//			}),
//			testingprotocol.SignMsg(t, secretKeys, []spectypes.OperatorID{spectypes.OperatorID(1)}, &specqbft.Message{
//				MsgType: message.CommitMsgType,
//				Height:  4,
//				Data:    commitDataToBytes(t, &specqbft.CommitData{Data: []byte("value")}),
//			}),
//			true,
//			"",
//			Ready,
//		},
//		{
//			"decided when init failed to sync",
//			nil,
//			testingprotocol.SignMsg(t, secretKeys, []spectypes.OperatorID{spectypes.OperatorID(1)}, &specqbft.Message{
//				MsgType: message.CommitMsgType,
//				Height:  1,
//				Data:    commitDataToBytes(t, &specqbft.CommitData{Data: []byte("value")}),
//			}),
//			testingprotocol.SignMsg(t, secretKeys, []spectypes.OperatorID{spectypes.OperatorID(1)}, &specqbft.Message{
//				MsgType: message.CommitMsgType,
//				Height:  1,
//				Data:    commitDataToBytes(t, &specqbft.CommitData{Data: []byte("value")}),
//			}),
//			true,
//			"",
//			NotStarted,
//		},
//		{
//			"decided from far future, requires sync.",
//			instance.NewInstanceWithState(&qbft.State{
//				Height: height3,
//			}),
//			testingprotocol.SignMsg(t, secretKeys, []spectypes.OperatorID{spectypes.OperatorID(1)}, &specqbft.Message{
//				MsgType: message.CommitMsgType,
//				Height:  2,
//				Data:    commitDataToBytes(t, &specqbft.CommitData{Data: []byte("value")}),
//			}),
//			testingprotocol.SignMsg(t, secretKeys, []spectypes.OperatorID{spectypes.OperatorID(1)}, &specqbft.Message{
//				MsgType: message.CommitMsgType,
//				Height:  10,
//				Data:    commitDataToBytes(t, &specqbft.CommitData{Data: []byte("value")}),
//			}),
//			true,
//			"",
//			Ready,
//		},
//		{
//			"decided from past, doesn't requires sync.",
//			instance.NewInstanceWithState(&qbft.State{
//				Height: height3,
//			}),
//			testingprotocol.SignMsg(t, secretKeys, []spectypes.OperatorID{spectypes.OperatorID(1)}, &specqbft.Message{
//				MsgType: message.CommitMsgType,
//				Height:  2,
//				Data:    commitDataToBytes(t, &specqbft.CommitData{Data: []byte("value")}),
//			}),
//			testingprotocol.SignMsg(t, secretKeys, []spectypes.OperatorID{spectypes.OperatorID(1)}, &specqbft.Message{
//				MsgType: message.CommitMsgType,
//				Height:  1,
//				Data:    commitDataToBytes(t, &specqbft.CommitData{Data: []byte("value")}),
//			}),
//			false,
//			"",
//			Ready,
//		},
//		{
//			"decided for current",
//			instance.NewInstanceWithState(&qbft.State{
//				Height: height3,
//			}),
//			testingprotocol.SignMsg(t, secretKeys, []spectypes.OperatorID{spectypes.OperatorID(1)}, &specqbft.Message{
//				MsgType: message.CommitMsgType,
//				Height:  2,
//				Data:    commitDataToBytes(t, &specqbft.CommitData{Data: []byte("value")}),
//			}),
//			testingprotocol.SignMsg(t, secretKeys, []spectypes.OperatorID{spectypes.OperatorID(1)}, &specqbft.Message{
//				MsgType: message.CommitMsgType,
//				Height:  1,
//				Data:    commitDataToBytes(t, &specqbft.CommitData{Data: []byte("value")}),
//			}),
//			false,
//			"",
//			Ready,
//		},
//		{
//			"decided for seq 0",
//			instance.NewInstanceWithState(&qbft.State{
//				Height: height0,
//			}),
//			nil,
//			testingprotocol.SignMsg(t, secretKeys, []spectypes.OperatorID{spectypes.OperatorID(1)}, &specqbft.Message{
//				MsgType: message.CommitMsgType,
//				Height:  0,
//				Data:    commitDataToBytes(t, &specqbft.CommitData{Data: []byte("value")}),
//			}),
//			false,
//			"",
//			Ready,
//		},
//	}
//
//	for _, test := range tests {
//		t.Run(test.name, func(t *testing.T) {
//			storage := newTestStorage(test.highestDecided)
//			currentInstanceLock := &sync.RWMutex{}
//			ctrl := Controller{
//				currentInstance:     test.currentInstance,
//				instanceStorage:     storage,
//				changeRoundStorage:  storage,
//				state:               test.initState,
//				currentInstanceLock: currentInstanceLock,
//				forkLock:            &sync.Mutex{},
//			}
//
//			ctrl.fork = forksfactory.NewFork(forksprotocol.GenesisForkVersion)
//			ctrl.decidedFactory = factory.NewDecidedFactory(zap.L(), ctrl.getNodeMode(), storage, nil)
//			ctrl.decidedStrategy = ctrl.decidedFactory.GetStrategy()
//
//			res, err := ctrl.decidedRequiresSync(test.msg)
//			require.EqualValues(t, test.expectedRes, res)
//			if len(test.expectedErr) > 0 {
//				require.EqualError(t, err, test.expectedErr)
//			} else {
//				require.NoError(t, err)
//			}
//		})
//	}
//}

//func TestDecideIsCurrentInstance(t *testing.T) {
//	uids := []spectypes.OperatorID{spectypes.OperatorID(1), spectypes.OperatorID(2), spectypes.OperatorID(3), spectypes.OperatorID(4)}
//	secretKeys, _ := testingprotocol.GenerateBLSKeys(uids...)
//
//	height1 := atomic.Value{}
//	height1.Store(specqbft.Height(1))
//
//	height4 := atomic.Value{}
//	height4.Store(specqbft.Height(4))
//
//	tests := []struct {
//		name            string
//		currentInstance instance.Instancer
//		msg             *specqbft.SignedMessage
//		expectedRes     bool
//	}{
//		{
//			"current instance",
//			instance.NewInstanceWithState(&qbft.State{
//				Height: height1,
//			}),
//			testingprotocol.SignMsg(t, secretKeys, []spectypes.OperatorID{spectypes.OperatorID(1)}, &specqbft.Message{
//				MsgType: message.CommitMsgType,
//				Height:  1,
//				Data:    commitDataToBytes(t, &specqbft.CommitData{Data: []byte("value")}),
//			}),
//			true,
//		},
//		{
//			"current instance nil",
//			nil,
//			testingprotocol.SignMsg(t, secretKeys, []spectypes.OperatorID{spectypes.OperatorID(1)}, &specqbft.Message{
//				MsgType: message.CommitMsgType,
//				Height:  1,
//				Data:    commitDataToBytes(t, &specqbft.CommitData{Data: []byte("value")}),
//			}),
//			false,
//		},
//		{
//			"current instance empty",
//			&instance.Instance{},
//			testingprotocol.SignMsg(t, secretKeys, []spectypes.OperatorID{spectypes.OperatorID(1)}, &specqbft.Message{
//				MsgType: message.CommitMsgType,
//				Height:  1,
//				Data:    commitDataToBytes(t, &specqbft.CommitData{Data: []byte("value")}),
//			}),
//			false,
//		},
//		{
//			"current instance seq lower",
//			instance.NewInstanceWithState(&qbft.State{
//				Height: height1,
//			}),
//			testingprotocol.SignMsg(t, secretKeys, []spectypes.OperatorID{spectypes.OperatorID(1)}, &specqbft.Message{
//				MsgType: message.CommitMsgType,
//				Height:  2,
//				Data:    commitDataToBytes(t, &specqbft.CommitData{Data: []byte("value")}),
//			}),
//			false,
//		},
//		{
//			"current instance seq higher",
//			instance.NewInstanceWithState(&qbft.State{
//				Height: height4,
//			}),
//			testingprotocol.SignMsg(t, secretKeys, []spectypes.OperatorID{spectypes.OperatorID(1)}, &specqbft.Message{
//				MsgType: message.CommitMsgType,
//				Height:  2,
//				Data:    commitDataToBytes(t, &specqbft.CommitData{Data: []byte("value")}),
//			}),
//			false,
//		},
//	}
//
//	for _, test := range tests {
//		t.Run(test.name, func(t *testing.T) {
//			currentInstanceLock := &sync.RWMutex{}
//			ibft := Controller{
//				currentInstance:     test.currentInstance,
//				currentInstanceLock: currentInstanceLock,
//				forkLock:            &sync.Mutex{},
//			}
//			require.EqualValues(t, test.expectedRes, ibft.decidedForCurrentInstance(test.msg))
//		})
//	}
//}

func TestForceDecided(t *testing.T) {
	uids := []spectypes.OperatorID{spectypes.OperatorID(1), spectypes.OperatorID(2), spectypes.OperatorID(3), spectypes.OperatorID(4)}
	sks, nodes := testingprotocol.GenerateBLSKeys(uids...)
	pi, err := protocolp2p.GenPeerID()
	require.NoError(t, err)

	network := protocolp2p.NewMockNetwork(zap.L(), pi, 10)

	identifier := spectypes.NewMsgID([]byte("Identifier_11"), spectypes.BNRoleAttester)
	s1 := testingprotocol.PopulatedStorage(t, sks, 3, 3)
	i1 := populatedIbft(1, identifier[:], network, s1, sks, nodes, newTestKeyManager())
	// test before sync
	highest, err := i1.(*Controller).DecidedStrategy.GetLastDecided(identifier)
	require.NotNil(t, highest)
	require.NoError(t, err)
	require.EqualValues(t, 3, highest.Message.Height)

	time.Sleep(time.Second * 1) // wait for sync to complete

	go func() {
		time.Sleep(time.Millisecond * 500) // wait for instance to start

		signers := []spectypes.OperatorID{spectypes.OperatorID(1), spectypes.OperatorID(2), spectypes.OperatorID(3), spectypes.OperatorID(4)}

		encodedCommit, err := (&specqbft.CommitData{Data: []byte("value")}).Encode()
		require.NoError(t, err)
		decidedMsg := testingprotocol.AggregateSign(t, sks, signers, &specqbft.Message{
			MsgType:    specqbft.CommitMsgType,
			Height:     specqbft.Height(4),
			Round:      specqbft.Round(1),
			Identifier: identifier[:],
			Data:       encodedCommit,
		})

		require.NoError(t, i1.(*Controller).processDecidedMessage(decidedMsg))
	}()

	res, err := i1.StartInstance(instance.ControllerStartInstanceOptions{
		Logger:    zap.L(),
		SeqNumber: 4,
		Value:     commitDataToBytes(t, &specqbft.CommitData{Data: []byte("value")}),
	})
	require.NoError(t, err)
	require.True(t, res.Decided)

	highest, err = i1.(*Controller).DecidedStrategy.GetLastDecided(identifier)
	require.NotNil(t, highest)
	require.NoError(t, err)
	require.EqualValues(t, 4, highest.Message.Height)
}

func TestSyncAfterDecided(t *testing.T) {
	uids := []spectypes.OperatorID{spectypes.OperatorID(1), spectypes.OperatorID(2), spectypes.OperatorID(3), spectypes.OperatorID(4)}
	sks, nodes := testingprotocol.GenerateBLSKeys(uids...)
	pi, err := protocolp2p.GenPeerID()
	require.NoError(t, err)

	identifier := spectypes.NewMsgID([]byte("Identifier_11"), spectypes.BNRoleAttester)

	decidedMsg := testingprotocol.AggregateSign(t, sks, uids, &specqbft.Message{
		MsgType:    specqbft.CommitMsgType,
		Height:     specqbft.Height(10),
		Round:      specqbft.Round(3),
		Identifier: identifier[:],
		Data:       commitDataToBytes(t, &specqbft.CommitData{Data: []byte("value")}),
	})

	network := protocolp2p.NewMockNetwork(zap.L(), pi, 10)
	network.SetLastDecidedHandler(generateLastDecidedHandler(t, identifier[:], decidedMsg))
	network.SetGetHistoryHandler(generateGetHistoryHandler(t, sks, uids, identifier[:], 4, 10))

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	network.Start(ctx)
	network.AddPeers(identifier.GetPubKey(), network)

	s1 := testingprotocol.PopulatedStorage(t, sks, 3, 4)
	i1 := populatedIbft(1, identifier[:], network, s1, sks, nodes, newTestKeyManager())

	_ = populatedIbft(2, identifier[:], network, testingprotocol.PopulatedStorage(t, sks, 3, 10), sks, nodes, newTestKeyManager())

	// test before sync
	highest, err := i1.(*Controller).DecidedStrategy.GetLastDecided(identifier)
	require.NotNil(t, highest)
	require.NoError(t, err)
	require.EqualValues(t, 4, highest.Message.Height)

	require.NoError(t, i1.(*Controller).processDecidedMessage(decidedMsg))

	time.Sleep(time.Millisecond * 500) // wait for sync to complete
	highest, err = i1.(*Controller).DecidedStrategy.GetLastDecided(identifier)
	require.NotNil(t, highest)
	require.NoError(t, err)
	require.EqualValues(t, specqbft.Height(10), highest.Message.Height)
}

func TestSyncFromScratchAfterDecided(t *testing.T) {
	uids := []spectypes.OperatorID{spectypes.OperatorID(1), spectypes.OperatorID(2), spectypes.OperatorID(3), spectypes.OperatorID(4)}
	sks, nodes := testingprotocol.GenerateBLSKeys(uids...)
	db, _ := kv.New(basedb.Options{
		Type:   "badger-memory",
		Path:   "",
		Logger: zap.L(),
	})
	pi, err := protocolp2p.GenPeerID()
	require.NoError(t, err)

	identifier := spectypes.NewMsgID([]byte("Identifier_11"), spectypes.BNRoleAttester)
	decidedMsg := testingprotocol.AggregateSign(t, sks, uids, &specqbft.Message{
		MsgType:    specqbft.CommitMsgType,
		Height:     specqbft.Height(10),
		Round:      specqbft.Round(3),
		Identifier: identifier[:],
		Data:       commitDataToBytes(t, &specqbft.CommitData{Data: []byte("value")}),
	})

	network := protocolp2p.NewMockNetwork(zap.L(), pi, 10)
	network.SetLastDecidedHandler(generateLastDecidedHandler(t, identifier[:], decidedMsg))
	network.SetGetHistoryHandler(generateGetHistoryHandler(t, sks, uids, identifier[:], 0, 10))

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	network.Start(ctx)
	network.AddPeers(identifier.GetPubKey(), network)

	s1 := qbftstorage.NewQBFTStore(db, zap.L(), "attestations")
	i1 := populatedIbft(1, identifier[:], network, s1, sks, nodes, newTestKeyManager())

	_ = populatedIbft(2, identifier[:], network, testingprotocol.PopulatedStorage(t, sks, 3, 10), sks, nodes, newTestKeyManager())

	require.NoError(t, i1.(*Controller).processDecidedMessage(decidedMsg))

	time.Sleep(time.Millisecond * 500) // wait for sync to complete
	highest, err := i1.(*Controller).DecidedStrategy.GetLastDecided(identifier)
	require.NotNil(t, highest)
	require.NoError(t, err)
	require.EqualValues(t, 10, highest.Message.Height)
}

func TestValidateDecidedMsg(t *testing.T) {
	uids := []spectypes.OperatorID{spectypes.OperatorID(1), spectypes.OperatorID(2), spectypes.OperatorID(3), spectypes.OperatorID(4)}
	sks, nodes := testingprotocol.GenerateBLSKeys(uids...)
	pi, err := protocolp2p.GenPeerID()
	require.NoError(t, err)

	network := protocolp2p.NewMockNetwork(zap.L(), pi, 10)
	identifier := []byte("Identifier_11")
	ibft := populatedIbft(1, identifier, network, testingprotocol.PopulatedStorage(t, sks, 3, 10), sks, nodes, newTestKeyManager())

	tests := []struct {
		name          string
		msg           *specqbft.SignedMessage
		expectedError error
	}{
		{
			"valid",
			testingprotocol.AggregateSign(t, sks, uids, &specqbft.Message{
				MsgType:    specqbft.CommitMsgType,
				Height:     specqbft.Height(11),
				Round:      specqbft.Round(3),
				Identifier: identifier,
				Data:       commitDataToBytes(t, &specqbft.CommitData{Data: []byte("value")}),
			}),
			nil,
		},
		{
			"invalid msg stage",
			testingprotocol.AggregateSign(t, sks, uids, &specqbft.Message{
				MsgType:    specqbft.PrepareMsgType,
				Height:     specqbft.Height(11),
				Round:      specqbft.Round(3),
				Identifier: identifier,
				Data:       commitDataToBytes(t, &specqbft.CommitData{Data: []byte("value")}),
			}),
			errors.New("message type is wrong"),
		},
		{
			"invalid msg sig",
			testingprotocol.AggregateInvalidSign(t, sks, &specqbft.Message{
				MsgType:    specqbft.CommitMsgType,
				Height:     specqbft.Height(11),
				Round:      specqbft.Round(3),
				Identifier: identifier,
				Data:       commitDataToBytes(t, &specqbft.CommitData{Data: []byte("value")}),
			}),
			errors.New("failed to verify signature"),
		},
		{
			"valid first decided",
			testingprotocol.AggregateSign(t, sks, uids, &specqbft.Message{
				MsgType:    specqbft.CommitMsgType,
				Height:     specqbft.Height(0),
				Round:      specqbft.Round(3),
				Identifier: identifier,
				Data:       commitDataToBytes(t, &specqbft.CommitData{Data: []byte("value")}),
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

//func TestController_checkDecidedMessageSigners(t *testing.T) {
//	uids := []spectypes.OperatorID{spectypes.OperatorID(1), spectypes.OperatorID(2), spectypes.OperatorID(3), spectypes.OperatorID(4)}
//	secretKeys, nodes := testingprotocol.GenerateBLSKeys(uids...)
//	skQuorum := map[spectypes.OperatorID]*bls.SecretKey{}
//	for i, sk := range secretKeys {
//		skQuorum[i] = sk
//	}
//	delete(skQuorum, 4)
//	identifier := []byte("Identifier_2")
//
//	incompleteDecided := testingprotocol.AggregateSign(t, skQuorum, uids[:3], &specqbft.Message{
//		MsgType:    message.CommitMsgType,
//		Height:     specqbft.Height(2),
//		Identifier: identifier[:],
//		Data:       commitDataToBytes(t, &specqbft.CommitData{Data: []byte("value")}),
//	})
//
//	completeDecided := testingprotocol.AggregateSign(t, secretKeys, uids, &specqbft.Message{
//		MsgType:    message.CommitMsgType,
//		Height:     specqbft.Height(2),
//		Identifier: identifier[:],
//		Data:       commitDataToBytes(t, &specqbft.CommitData{Data: []byte("value")}),
//	})
//
//	share := &beaconprotocol.Share{
//		NodeID:    1,
//		PublicKey: secretKeys[1].GetPublicKey(),
//		Committee: nodes,
//	}
//
//	id := atomic.Value{}
//	id.Store(message.Identifier(identifier))
//
//	height := atomic.Value{}
//	height.Store(specqbft.Height(2))
//
//	storage := newTestStorage(nil)
//	currentInstanceLock := &sync.RWMutex{}
//	ctrl := Controller{
//		ValidatorShare: share,
//		currentInstance: instance.NewInstanceWithState(&qbft.State{
//			Identifier: id,
//			Height:     height,
//		}),
//		instanceStorage:     storage,
//		changeRoundStorage:  storage,
//		currentInstanceLock: currentInstanceLock,
//		forkLock:            &sync.Mutex{},
//	}
//
//	ctrl.fork = forksfactory.NewFork(forksprotocol.GenesisForkVersion)
//	ctrl.decidedFactory = factory.NewDecidedFactory(zap.L(), ctrl.getNodeMode(), storage, nil)
//	ctrl.decidedStrategy = ctrl.decidedFactory.GetStrategy()
//
//	_, err := ctrl.decidedStrategy.SaveDecided(incompleteDecided)
//	require.NoError(t, err)
//
//	// check message with similar number of signers
//	require.True(t, ctrl.checkDecidedMessageSigners(incompleteDecided, incompleteDecided))
//	// check message with more signers
//	require.False(t, ctrl.checkDecidedMessageSigners(incompleteDecided, completeDecided))
//}

// TODO: (lint) fix test
//nolint
func populatedIbft(
	nodeID spectypes.OperatorID,
	identifier []byte,
	network protocolp2p.MockNetwork,
	ibftStorage qbftstorage.QBFTStore,
	sks map[spectypes.OperatorID]*bls.SecretKey,
	nodes map[spectypes.OperatorID]*beaconprotocol.Node,
	keyManager spectypes.KeyManager,
) IController {
	share := &beaconprotocol.Share{
		NodeID:      nodeID,
		PublicKey:   sks[1].GetPublicKey(),
		Committee:   nodes,
		OperatorIds: []uint64{1, 2, 3, 4},
	}

	opts := Options{
		Context:        context.Background(),
		Role:           spectypes.BNRoleAttester,
		Identifier:     message.ToMessageID(identifier),
		Logger:         zap.L(),
		Storage:        ibftStorage,
		Network:        network,
		InstanceConfig: qbft.DefaultConsensusParams(),
		ValidatorShare: share,
		Version:        forksprotocol.GenesisForkVersion, // TODO need to check v1 fork too? (:Niv)
		Beacon:         nil,                              // ?
		KeyManager:     keyManager,
		SyncRateLimit:  time.Millisecond * 100,
		SigTimeout:     time.Second * 5,
		ReadMode:       false,
	}
	ret := New(opts)

	ret.(*Controller).State = Ready // as if they are already synced
	return ret
}

type testSSVSigner struct {
}

func newTestKeyManager() spectypes.KeyManager {
	return &testSSVSigner{}
}

func (s *testSSVSigner) Decrypt(pk *rsa.PublicKey, cipher []byte) ([]byte, error) {
	panic("implement me")
}

func (s *testSSVSigner) Encrypt(pk *rsa.PublicKey, data []byte) ([]byte, error) {
	panic("implement me")
}

func (s *testSSVSigner) SignRoot(data spectypes.Root, sigType spectypes.SignatureType, pk []byte) (spectypes.Signature, error) {
	return nil, nil
}

func (s *testSSVSigner) AddShare(shareKey *bls.SecretKey) error {
	return nil
}

func (s *testSSVSigner) SignAttestation(data *spec.AttestationData, duty *spectypes.Duty, pk []byte) (*spec.Attestation, []byte, error) {
	panic("implement me")
}

func (s *testSSVSigner) IsAttestationSlashable(data *spec.AttestationData) error {
	panic("implement me")
}

func (s *testSSVSigner) SignRandaoReveal(epoch spec.Epoch, pk []byte) (spectypes.Signature, []byte, error) {
	panic("implement me")
}

func (s *testSSVSigner) IsBeaconBlockSlashable(block *altair.BeaconBlock) error {
	panic("implement me")
}

func (s *testSSVSigner) SignBeaconBlock(block *altair.BeaconBlock, duty *spectypes.Duty, pk []byte) (*altair.SignedBeaconBlock, []byte, error) {
	panic("implement me")
}

func (s *testSSVSigner) SignSlotWithSelectionProof(slot spec.Slot, pk []byte) (spectypes.Signature, []byte, error) {
	panic("implement me")
}

func (s *testSSVSigner) SignAggregateAndProof(msg *spec.AggregateAndProof, duty *spectypes.Duty, pk []byte) (*spec.SignedAggregateAndProof, []byte, error) {
	panic("implement me")
}

func (s *testSSVSigner) SignSyncCommitteeBlockRoot(slot spec.Slot, root spec.Root, validatorIndex spec.ValidatorIndex, pk []byte) (*altair.SyncCommitteeMessage, []byte, error) {
	panic("implement me")
}

func (s *testSSVSigner) SignContributionProof(slot spec.Slot, index uint64, pk []byte) (spectypes.Signature, []byte, error) {
	panic("implement me")
}

func (s *testSSVSigner) SignContribution(contribution *altair.ContributionAndProof, pk []byte) (*altair.SignedContributionAndProof, []byte, error) {
	panic("implement me")
}

func (s *testSSVSigner) RemoveShare(pubKey string) error {
	panic("implement me")
}

func commitDataToBytes(t *testing.T, input *specqbft.CommitData) []byte {
	ret, err := input.Encode()
	require.NoError(t, err)
	return ret
}

func generateGetHistoryHandler(t *testing.T, sks map[spectypes.OperatorID]*bls.SecretKey, uids []spectypes.OperatorID, identifier []byte, from, to int) protocolp2p.EventHandler {
	return func(e protocolp2p.MockMessageEvent) *spectypes.SSVMessage {
		decidedMsgs := make([]*specqbft.SignedMessage, 0)
		heights := make([]specqbft.Height, 0)
		for i := from; i <= to; i++ {
			decidedMsgs = append(decidedMsgs, testingprotocol.AggregateSign(t, sks, uids, &specqbft.Message{
				MsgType:    specqbft.CommitMsgType,
				Height:     specqbft.Height(i),
				Round:      specqbft.Round(3),
				Identifier: identifier,
				Data:       commitDataToBytes(t, &specqbft.CommitData{Data: []byte("value")}),
			}))
			heights = append(heights, specqbft.Height(i))
		}

		sm := &message.SyncMessage{
			Protocol: message.LastDecidedType,
			Params: &message.SyncParams{
				Height:     heights,
				Identifier: message.ToMessageID(identifier),
			},
			Data:   decidedMsgs,
			Status: message.StatusSuccess,
		}
		em, err := sm.Encode()
		require.NoError(t, err)

		msg := &spectypes.SSVMessage{
			MsgType: spectypes.SSVDecidedMsgType,
			MsgID:   message.ToMessageID(identifier),
			Data:    em,
		}

		return msg
	}
}

func generateLastDecidedHandler(t *testing.T, identifier []byte, decidedMsg *specqbft.SignedMessage) protocolp2p.EventHandler {
	return func(e protocolp2p.MockMessageEvent) *spectypes.SSVMessage {
		sm := &message.SyncMessage{
			Protocol: message.LastDecidedType,
			Params: &message.SyncParams{
				Height:     []specqbft.Height{specqbft.Height(9), specqbft.Height(10)},
				Identifier: message.ToMessageID(identifier),
			},
			Data:   []*specqbft.SignedMessage{decidedMsg},
			Status: message.StatusSuccess,
		}
		em, err := sm.Encode()
		require.NoError(t, err)

		msg := &spectypes.SSVMessage{
			MsgType: spectypes.SSVDecidedMsgType,
			MsgID:   message.ToMessageID(identifier),
			Data:    em,
		}

		return msg
	}
}
