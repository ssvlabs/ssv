package controller
//
//import (
//	"fmt"
//	forksprotocol "github.com/bloxapp/ssv/protocol/forks"
//	protocolp2p "github.com/bloxapp/ssv/protocol/v1/p2p"
//	"sync"
//	"testing"
//	"time"
//
//	"github.com/bloxapp/ssv/beacon/valcheck"
//	"github.com/bloxapp/ssv/network/local"
//	beaconprotocol "github.com/bloxapp/ssv/protocol/v1/blockchain/beacon"
//	"github.com/bloxapp/ssv/protocol/v1/message"
//	"github.com/bloxapp/ssv/protocol/v1/qbft"
//	"github.com/bloxapp/ssv/protocol/v1/qbft/instance"
//	qbftstorage "github.com/bloxapp/ssv/protocol/v1/qbft/storage"
//	testingprotocol "github.com/bloxapp/ssv/protocol/v1/testing"
//	"github.com/bloxapp/ssv/storage/basedb"
//	"github.com/bloxapp/ssv/storage/collections"
//	"github.com/bloxapp/ssv/storage/kv"
//	"github.com/bloxapp/ssv/utils/logex"
//	"github.com/herumi/bls-eth-go-binary/bls"
//	"github.com/pkg/errors"
//	"github.com/stretchr/testify/require"
//
//	"go.uber.org/atomic"
//	"go.uber.org/zap"
//)
//
//type testStorage struct {
//	highestDecided *message.SignedMessage
//	msgs           map[string]*message.SignedMessage
//	lock           sync.Mutex
//}
//
//func newTestStorage(highestDecided *message.SignedMessage) qbftstorage.QBFTStore {
//	return &testStorage{
//		highestDecided: highestDecided,
//		msgs:           map[string]*message.SignedMessage{},
//		lock:           sync.Mutex{},
//	}
//}
//
//func msgKey(identifier []byte, Height message.Height) string {
//	return fmt.Sprintf("%s_%d", string(identifier), Height)
//}
//
//func (s *testStorage) GetLastDecided(identifier message.Identifier) (*message.SignedMessage, error) {
//	return s.highestDecided, nil
//}
//
//// SaveLastDecided saves the given decided message, after checking that it is indeed the highest
//func (s *testStorage) SaveLastDecided(signedMsg ...*message.SignedMessage) error {
//	return nil
//}
//
//// GetDecided returns historical decided messages in the given range
//func (s *testStorage) GetDecided(identifier message.Identifier, from message.Height, to message.Height) ([]*message.SignedMessage, error) {
//	s.lock.Lock()
//	defer s.lock.Unlock()
//
//	var msgs []*message.SignedMessage
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
//func (s *testStorage) SaveDecided(signedMsg ...*message.SignedMessage) error {
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
//func (s *testStorage) GetLastChangeRoundMsg(identifier message.Identifier) (*message.SignedMessage, error) {
//	return nil, nil
//}
//
//// SaveLastChangeRoundMsg returns the latest broadcasted msg from the instance
//func (s *testStorage) SaveLastChangeRoundMsg(identifier message.Identifier, msg *message.SignedMessage) error {
//	return nil
//}
//
//func newHeight(height int64) atomic.Value {
//	res := atomic.Value{}
//	res.Store(height)
//	return res
//}
//
//func newIdentifier(identity []byte) atomic.Value {
//	res := atomic.Value{}
//	res.Store(identity)
//	return res
//}
//
//func TestDecidedRequiresSync(t *testing.T) {
//	uids := []message.OperatorID{message.OperatorID(1), message.OperatorID(2), message.OperatorID(3), message.OperatorID(4)}
//	secretKeys, _ := testingprotocol.GenerateBLSKeys(uids...)
//	tests := []struct {
//		name            string
//		currentInstance instance.Instancer
//		highestDecided  *message.SignedMessage
//		msg             *message.SignedMessage
//		expectedRes     bool
//		expectedErr     string
//		initSynced      bool
//	}{
//		{
//			"decided from future, requires sync.",
//			instance.NewInstanceWithState(&qbft.State{
//				Identifier: atomic.Value{},
//				Height:     newHeight(3),
//			}),
//			testingprotocol.SignMsg(t, secretKeys, []message.OperatorID{message.OperatorID(1)}, &message.ConsensusMessage{
//				MsgType: message.CommitMsgType,
//				Height:  2,
//			}),
//			testingprotocol.SignMsg(t, secretKeys, []message.OperatorID{message.OperatorID(1)}, &message.ConsensusMessage{
//				MsgType: message.CommitMsgType,
//				Height:  4,
//			}),
//			true,
//			"",
//			true,
//		},
//		{
//			"decided from future, requires sync. current is nil",
//			nil,
//			testingprotocol.SignMsg(t, secretKeys, []message.OperatorID{message.OperatorID(1)}, &message.ConsensusMessage{
//				MsgType: message.CommitMsgType,
//				Height:  2,
//			}),
//			testingprotocol.SignMsg(t, secretKeys, []message.OperatorID{message.OperatorID(1)}, &message.ConsensusMessage{
//				MsgType: message.CommitMsgType,
//				Height:  4,
//			}),
//			true,
//			"",
//			true,
//		},
//		{
//			"decided when init failed to sync",
//			nil,
//			testingprotocol.SignMsg(t, secretKeys, []message.OperatorID{message.OperatorID(1)}, &message.ConsensusMessage{
//				MsgType: message.CommitMsgType,
//				Height:  1,
//			}),
//			testingprotocol.SignMsg(t, secretKeys, []message.OperatorID{message.OperatorID(1)}, &message.ConsensusMessage{
//				MsgType: message.CommitMsgType,
//				Height:  1,
//			}),
//			true,
//			"",
//			false,
//		},
//		{
//			"decided from far future, requires sync.",
//			instance.NewInstanceWithState(&qbft.State{
//				Identifier: atomic.Value{},
//				Height:     newHeight(3),
//			}),
//			testingprotocol.SignMsg(t, secretKeys, []message.OperatorID{message.OperatorID(1)}, &message.ConsensusMessage{
//				MsgType: message.CommitMsgType,
//				Height:  2,
//			}),
//			testingprotocol.SignMsg(t, secretKeys, []message.OperatorID{message.OperatorID(1)}, &message.ConsensusMessage{
//				MsgType: message.CommitMsgType,
//				Height:  10,
//			}),
//			true,
//			"",
//			true,
//		},
//		{
//			"decided from past, doesn't requires sync.",
//			instance.NewInstanceWithState(&qbft.State{
//				Identifier: atomic.Value{},
//				Height:     newHeight(3),
//			}),
//			testingprotocol.SignMsg(t, secretKeys, []message.OperatorID{message.OperatorID(1)}, &message.ConsensusMessage{
//				MsgType: message.CommitMsgType,
//				Height:  2,
//			}),
//			testingprotocol.SignMsg(t, secretKeys, []message.OperatorID{message.OperatorID(1)}, &message.ConsensusMessage{
//				MsgType: message.CommitMsgType,
//				Height:  1,
//			}),
//			false,
//			"",
//			true,
//		},
//		{
//			"decided for current",
//			instance.NewInstanceWithState(&qbft.State{
//				Identifier: atomic.Value{},
//				Height:     newHeight(3),
//			}),
//			testingprotocol.SignMsg(t, secretKeys, []message.OperatorID{message.OperatorID(1)}, &message.ConsensusMessage{
//				MsgType: message.CommitMsgType,
//				Height:  2,
//			}),
//			testingprotocol.SignMsg(t, secretKeys, []message.OperatorID{message.OperatorID(1)}, &message.ConsensusMessage{
//				MsgType: message.CommitMsgType,
//				Height:  1,
//			}),
//			false,
//			"",
//			true,
//		},
//		{
//			"decided for seq 0",
//			instance.NewInstanceWithState(&qbft.State{
//				Identifier: atomic.Value{},
//				Height:     newHeight(0),
//			}),
//			nil,
//			testingprotocol.SignMsg(t, secretKeys, []message.OperatorID{message.OperatorID(1)}, &message.ConsensusMessage{
//				MsgType: message.CommitMsgType,
//				Height:  0,
//			}),
//			false,
//			"",
//			true,
//		},
//	}
//
//	for _, test := range tests {
//		t.Run(test.name, func(t *testing.T) {
//			ctrl := Controller{
//				currentInstance: test.currentInstance,
//				ibftStorage:     newTestStorage(test.highestDecided),
//				initSynced:      atomic.Bool{},
//			}
//			ctrl.initSynced.Store(test.initSynced)
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
//
//func TestDecideIsCurrentInstance(t *testing.T) {
//	uids := []message.OperatorID{message.OperatorID(1), message.OperatorID(2), message.OperatorID(3), message.OperatorID(4)}
//	secretKeys, _ := testingprotocol.GenerateBLSKeys(uids...)
//	tests := []struct {
//		name            string
//		currentInstance instance.Instancer
//		msg             *message.SignedMessage
//		expectedRes     bool
//	}{
//		{
//			"current instance",
//			instance.NewInstanceWithState(&qbft.State{
//				Identifier: atomic.Value{},
//				Height:     newHeight(1),
//			}),
//			testingprotocol.SignMsg(t, secretKeys, []message.OperatorID{message.OperatorID(1)}, &message.ConsensusMessage{
//				MsgType: message.CommitMsgType,
//				Height:  1,
//			}),
//			true,
//		},
//		{
//			"current instance nil",
//			&instance.Instance{},
//			testingprotocol.SignMsg(t, secretKeys, []message.OperatorID{message.OperatorID(1)}, &message.ConsensusMessage{
//				MsgType: message.CommitMsgType,
//				Height:  1,
//			}),
//			false,
//		},
//		{
//			"current instance seq lower",
//			instance.NewInstanceWithState(&qbft.State{
//				Identifier: atomic.Value{},
//				Height:     newHeight(1),
//			}),
//			testingprotocol.SignMsg(t, secretKeys, []message.OperatorID{message.OperatorID(1)}, &message.ConsensusMessage{
//				MsgType: message.CommitMsgType,
//				Height:  2,
//			}),
//			false,
//		},
//		{
//			"current instance seq higher",
//			instance.NewInstanceWithState(&qbft.State{
//				Identifier: atomic.Value{},
//				Height:     newHeight(4),
//			}),
//			testingprotocol.SignMsg(t, secretKeys, []message.OperatorID{message.OperatorID(1)}, &message.ConsensusMessage{
//				MsgType: message.CommitMsgType,
//				Height:  2,
//			}),
//			false,
//		},
//	}
//
//	for _, test := range tests {
//		t.Run(test.name, func(t *testing.T) {
//			ibft := Controller{currentInstance: test.currentInstance}
//			require.EqualValues(t, test.expectedRes, ibft.decidedForCurrentInstance(test.msg))
//		})
//	}
//}
//
//func TestForceDecided(t *testing.T) { // TODo need to align with the new queue process
//	uids := []message.OperatorID{message.OperatorID(1), message.OperatorID(2), message.OperatorID(3), message.OperatorID(4)}
//	sks, nodes := testingprotocol.GenerateBLSKeys(uids...)
//	pi, err := protocolp2p.GenPeerID()
//	require.NoError(t, err)
//	network := protocolp2p.NewMockNetwork(logex.GetLogger(), pi, 10)
//
//	identifier := []byte("Identifier_11")
//	s1 := testingprotocol.PopulatedStorage(t, sks, 3, 3)
//	i1 := populatedIbft(1, identifier, network, s1, sks, nodes, newTestSigner())
//	// test before sync
//	highest, err := i1.(*Controller).ibftStorage.GetLastDecided(identifier)
//	require.NotNil(t, highest)
//	require.NoError(t, err)
//	require.EqualValues(t, 3, highest.Message.Height)
//
//	time.Sleep(time.Second * 1) // wait for sync to complete
//
//	go func() {
//		time.Sleep(time.Millisecond * 500) // wait for instance to start
//
//		signers := []message.OperatorID{message.OperatorID(1), message.OperatorID(2), message.OperatorID(3), message.OperatorID(4)}
//		decidedMsg := testingprotocol.AggregateSign(t, sks, signers, &message.ConsensusMessage{
//			MsgType:    message.CommitMsgType,
//			Height:     message.Height(4),
//			Round:      message.Round(1),
//			Identifier: []byte("value"),
//			Data:       []byte("value"),
//		})
//
//		i1.(*Controller).ProcessDecidedMessage(decidedMsg)
//	}()
//
//	res, err := i1.StartInstance(instance.ControllerStartInstanceOptions{
//		Logger:     logex.GetLogger(),
//		ValueCheck: &valcheck.AttestationValueCheck{},
//		SeqNumber:  4,
//		Value:      []byte("value"),
//	})
//	require.NoError(t, err)
//	require.True(t, res.Decided)
//
//	highest, err = i1.(*Controller).ibftStorage.GetLastDecided(identifier)
//	require.NotNil(t, highest)
//	require.NoError(t, err)
//	require.EqualValues(t, 4, highest.Message.Height)
//}
//
//func TestSyncAfterDecided(t *testing.T) {
//	uids := []message.OperatorID{message.OperatorID(1), message.OperatorID(2), message.OperatorID(3), message.OperatorID(4)}
//	sks, nodes := testingprotocol.GenerateBLSKeys(uids...)
//	pi, err := protocolp2p.GenPeerID()
//	require.NoError(t, err)
//	network := protocolp2p.NewMockNetwork(logex.GetLogger(), pi, 10)
//
//	identifier := []byte("Identifier_11")
//	s1 := testingprotocol.PopulatedStorage(t, sks, 3, 4)
//	i1 := populatedIbft(1, identifier, network, s1, sks, nodes, newTestSigner())
//
//	_ = populatedIbft(2, identifier, network, testingprotocol.PopulatedStorage(t, sks, 3, 10), sks, nodes, newTestSigner())
//
//	// test before sync
//	highest, err := i1.(*Controller).ibftStorage.GetLastDecided(identifier)
//	require.NotNil(t, highest)
//	require.NoError(t, err)
//	require.EqualValues(t, 4, highest.Message.Height)
//
//	decidedMsg := testingprotocol.AggregateSign(t, sks, uids, &message.ConsensusMessage{
//		MsgType:    message.CommitMsgType,
//		Height:     message.Height(10),
//		Round:      message.Round(3),
//		Identifier: identifier,
//		Data:       []byte("value"),
//	})
//
//	i1.(*Controller).ProcessDecidedMessage(decidedMsg)
//
//	time.Sleep(time.Millisecond * 500) // wait for sync to complete
//	highest, err = i1.(*Controller).ibftStorage.GetLastDecided(identifier)
//	require.NotNil(t, highest)
//	require.NoError(t, err)
//	require.EqualValues(t, 10, highest.Message.Height)
//}
//
//func TestSyncFromScratchAfterDecided(t *testing.T) {
//	uids := []message.OperatorID{message.OperatorID(1), message.OperatorID(2), message.OperatorID(3), message.OperatorID(4)}
//	sks, nodes := testingprotocol.GenerateBLSKeys(uids...)
//	db, _ := kv.New(basedb.Options{
//		Type:   "badger-memory",
//		Path:   "",
//		Logger: zap.L(),
//	})
//	pi, err := protocolp2p.GenPeerID()
//	require.NoError(t, err)
//	network := protocolp2p.NewMockNetwork(logex.GetLogger(), pi, 10)
//
//	identifier := []byte("Identifier_11")
//	s1 := collections.NewIbft(db, zap.L(), "attestation")
//	i1 := populatedIbft(1, identifier, network, &s1, sks, nodes, newTestSigner())
//
//	_ = populatedIbft(2, identifier, network, testingprotocol.PopulatedStorage(t, sks, 3, 10), sks, nodes, newTestSigner())
//
//	decidedMsg := testingprotocol.AggregateSign(t, sks, uids, &message.ConsensusMessage{
//		MsgType:    message.CommitMsgType,
//		Height:     message.Height(10),
//		Round:      message.Round(3),
//		Identifier: identifier,
//		Data:       []byte("value"),
//	})
//
//	i1.(*Controller).ProcessDecidedMessage(decidedMsg)
//
//	time.Sleep(time.Millisecond * 500) // wait for sync to complete
//	highest, err := i1.(*Controller).ibftStorage.GetLastDecided(identifier)
//	require.NotNil(t, highest)
//	require.NoError(t, err)
//	require.EqualValues(t, 10, highest.Message.Height)
//}
//
//func TestValidateDecidedMsg(t *testing.T) {
//	uids := []message.OperatorID{message.OperatorID(1), message.OperatorID(2), message.OperatorID(3), message.OperatorID(4)}
//	sks, nodes := testingprotocol.GenerateBLSKeys(uids...)
//	pi, err := protocolp2p.GenPeerID()
//	require.NoError(t, err)
//	network := protocolp2p.NewMockNetwork(logex.GetLogger(), pi, 10)
//	identifier := []byte("Identifier_11")
//	ibft := populatedIbft(1, identifier, network, testingprotocol.PopulatedStorage(t, sks, 3, 10), sks, nodes, newTestSigner())
//
//	tests := []struct {
//		name          string
//		msg           *message.SignedMessage
//		expectedError error
//	}{
//		{
//			"valid",
//			testingprotocol.AggregateSign(t, sks, uids, &message.ConsensusMessage{
//				MsgType:    message.CommitMsgType,
//				Height:     message.Height(11),
//				Round:      message.Round(3),
//				Identifier: identifier,
//				Data:       []byte("value"),
//			}),
//			nil,
//		},
//		{
//			"invalid msg stage",
//			testingprotocol.AggregateSign(t, sks, uids, &message.ConsensusMessage{
//				MsgType:    message.PrepareMsgType,
//				Height:     message.Height(11),
//				Round:      message.Round(3),
//				Identifier: identifier,
//				Data:       []byte("value"),
//			}),
//			errors.New("message type is wrong"),
//		},
//		{
//			"invalid msg sig",
//			testingprotocol.AggregateInvalidSign(t, sks, &message.ConsensusMessage{
//				MsgType:    message.CommitMsgType,
//				Height:     message.Height(11),
//				Round:      message.Round(3),
//				Identifier: identifier,
//				Data:       []byte("value"),
//			}),
//			errors.New("could not verify message signature"),
//		},
//		{
//			"valid first decided",
//			testingprotocol.AggregateSign(t, sks, uids, &message.ConsensusMessage{
//				MsgType:    message.CommitMsgType,
//				Height:     message.Height(0),
//				Round:      message.Round(3),
//				Identifier: identifier,
//				Data:       []byte("value"),
//			}),
//			nil,
//		},
//	}
//
//	for _, test := range tests {
//		t.Run(test.name, func(t *testing.T) {
//			if test.expectedError != nil {
//				err := ibft.(*Controller).ValidateDecidedMsg(test.msg)
//				require.EqualError(t, err, test.expectedError.Error())
//			} else {
//				require.NoError(t, ibft.(*Controller).ValidateDecidedMsg(test.msg))
//			}
//		})
//	}
//}
//
//func TestController_checkDecidedMessageSigners(t *testing.T) {
//	uids := []message.OperatorID{message.OperatorID(1), message.OperatorID(2), message.OperatorID(3), message.OperatorID(4)}
//	secretKeys, nodes := testingprotocol.GenerateBLSKeys(uids...)
//	skQuorum := map[uint64]*bls.SecretKey{}
//	for i, sk := range secretKeys {
//		skQuorum[uint64(i)] = sk
//	}
//	delete(skQuorum, uint64(4))
//	identifier := []byte("Identifier_2")
//
//	incompleteDecided := testingprotocol.AggregateSign(t, secretKeys, uids, &message.ConsensusMessage{
//		MsgType:    message.CommitMsgType,
//		Height:     message.Height(2),
//		Identifier: identifier[:],
//	})
//
//	completeDecided := testingprotocol.AggregateSign(t, secretKeys, uids, &message.ConsensusMessage{
//		MsgType:    message.CommitMsgType,
//		Height:     message.Height(2),
//		Identifier: identifier[:],
//	})
//
//	share := &beaconprotocol.Share{
//		NodeID:    1,
//		PublicKey: secretKeys[1].GetPublicKey(),
//		Committee: nodes,
//	}
//
//	ctrl := Controller{
//		ValidatorShare: share,
//		currentInstance: instance.NewInstanceWithState(&qbft.State{
//			Identifier: newIdentifier(identifier),
//			Height:     newHeight(2),
//		}),
//		ibftStorage: newTestStorage(nil),
//	}
//	require.NoError(t, ctrl.ibftStorage.SaveDecided(incompleteDecided))
//	// check message with similar number of signers
//	shouldIgnore, err := ctrl.checkDecidedMessageSigners(incompleteDecided)
//	require.NoError(t, err)
//	require.True(t, shouldIgnore)
//	// check message with more signers
//	shouldIgnore, err = ctrl.checkDecidedMessageSigners(completeDecided)
//	require.NoError(t, err)
//	require.False(t, shouldIgnore)
//}
//
//func populatedIbft(
//	nodeID message.OperatorID,
//	identifier []byte,
//	network protocolp2p.MockNetwork,
//	ibftStorage qbftstorage.QBFTStore,
//	sks map[message.OperatorID]*bls.SecretKey,
//	nodes map[message.OperatorID]*beaconprotocol.Node,
//	signer beaconprotocol.Signer,
//) IController {
//	share := &beaconprotocol.Share{
//		NodeID:    nodeID,
//		PublicKey: sks[1].GetPublicKey(),
//		Committee: nodes,
//	}
//
//	opts := Options{
//		Role:           message.RoleTypeAttester,
//		Identifier:     identifier,
//		Logger:         logex.GetLogger(),
//		Storage:        ibftStorage,
//		Network:        network,
//		InstanceConfig: qbft.DefaultConsensusParams(),
//		ValidatorShare: share,
//		Version:        forksprotocol.V0ForkVersion, // TODO need to check v1 fork too? (:Niv)
//		Beacon:         nil,                         // ?
//		Signer:         signer,
//		SyncRateLimit:  time.Millisecond * 100,
//		SigTimeout:     time.Second * 5,
//		ReadMode:       false,
//	}
//	ret := New(opts)
//
//	ret.(*Controller).initHandlers.Store(true) // as if they are already synced
//	ret.(*Controller).initSynced.Store(true)   // as if they are already synced
//	return ret
//}
