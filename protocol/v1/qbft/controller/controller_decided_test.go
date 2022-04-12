package controller

import (
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/bloxapp/ssv/beacon/valcheck"
	"github.com/bloxapp/ssv/ibft/proto"
	"github.com/bloxapp/ssv/network/local"
	beaconprotocol "github.com/bloxapp/ssv/protocol/v1/blockchain/beacon"
	"github.com/bloxapp/ssv/protocol/v1/message"
	"github.com/bloxapp/ssv/protocol/v1/qbft"
	"github.com/bloxapp/ssv/protocol/v1/qbft/instance"
	qbftstorage "github.com/bloxapp/ssv/protocol/v1/qbft/storage"
	testing2 "github.com/bloxapp/ssv/protocol/v1/testing"
	"github.com/bloxapp/ssv/storage/basedb"
	"github.com/bloxapp/ssv/storage/collections"
	"github.com/bloxapp/ssv/storage/kv"
	"github.com/bloxapp/ssv/utils/logex"
	"github.com/bloxapp/ssv/validator/storage"
	"github.com/herumi/bls-eth-go-binary/bls"
	"github.com/pkg/errors"
	"github.com/stretchr/testify/require"

	"go.uber.org/atomic"
	"go.uber.org/zap"
)

type testStorage struct {
	highestDecided *message.SignedMessage
	msgs           map[string]*message.SignedMessage
	lock           sync.Mutex
}

func newTestStorage(highestDecided *message.SignedMessage) qbftstorage.QBFTStore {
	return &testStorage{
		highestDecided: highestDecided,
		msgs:           map[string]*message.SignedMessage{},
		lock:           sync.Mutex{},
	}
}

func msgKey(identifier []byte, Height message.Height) string {
	return fmt.Sprintf("%s_%d", string(identifier), Height)
}

func (s *testStorage) GetLastDecided(identifier message.Identifier) (*message.SignedMessage, error) {
	return s.highestDecided, nil
}

// SaveLastDecided saves the given decided message, after checking that it is indeed the highest
func (s *testStorage) SaveLastDecided(signedMsg ...*message.SignedMessage) error {
	return nil
}

// GetDecided returns historical decided messages in the given range
func (s *testStorage) GetDecided(identifier message.Identifier, from message.Height, to message.Height) ([]*message.SignedMessage, error) {
	s.lock.Lock()
	defer s.lock.Unlock()

	var msgs []*message.SignedMessage
	for i := from; i <= to; i++ {
		k := msgKey(identifier, i)
		if msg, ok := s.msgs[k]; ok {
			msgs = append(msgs, msg)
		}
	}
	return msgs, nil
}

// SaveDecided saves historical decided messages
func (s *testStorage) SaveDecided(signedMsg ...*message.SignedMessage) error {
	s.lock.Lock()
	defer s.lock.Unlock()

	for _, msg := range signedMsg {
		if msg == nil || msg.Message == nil {
			continue
		}
		k := msgKey(msg.Message.Identifier, msg.Message.Height)
		s.msgs[k] = msg
	}
	return nil
}

// SaveCurrentInstance saves the state for the current running (not yet decided) instance
func (s *testStorage) SaveCurrentInstance(identifier message.Identifier, state *qbft.State) error {
	return nil
}

// GetCurrentInstance returns the state for the current running (not yet decided) instance
func (s *testStorage) GetCurrentInstance(identifier message.Identifier) (*qbft.State, bool, error) {
	return nil, false, nil
}

// GetLastChangeRoundMsg returns the latest broadcasted msg from the instance
func (s *testStorage) GetLastChangeRoundMsg(identifier message.Identifier) (*message.SignedMessage, error) {
	return nil, nil
}

// SaveLastChangeRoundMsg returns the latest broadcasted msg from the instance
func (s *testStorage) SaveLastChangeRoundMsg(identifier message.Identifier, msg *message.SignedMessage) error {
	return nil
}

func newHeight(height int64) atomic.Value {
	res := atomic.Value{}
	res.Store(height)
	return res
}

func newIdentifier(identity []byte) atomic.Value {
	res := atomic.Value{}
	res.Store(identity)
	return res
}

func SignMsg(t *testing.T, sks map[message.OperatorID]*bls.SecretKey, signers []message.OperatorID, msg *message.ConsensusMessage) *message.SignedMessage {
	res, err := testing2.MultiSignMsg(sks, signers, msg)
	require.NoError(t, err)
	return res
}

func TestDecidedRequiresSync(t *testing.T) {
	uids := []message.OperatorID{message.OperatorID(1), message.OperatorID(2), message.OperatorID(3), message.OperatorID(4)}
	secretKeys, _ := testing2.GenerateBLSKeys(uids...)
	tests := []struct {
		name            string
		currentInstance instance.Instancer
		highestDecided  *message.SignedMessage
		msg             *message.SignedMessage
		expectedRes     bool
		expectedErr     string
		initSynced      bool
	}{
		{
			"decided from future, requires sync.",
			instance.NewInstanceWithState(&qbft.State{
				Identifier: atomic.Value{},
				Height:     newHeight(3),
			}),
			SignMsg(t, secretKeys, []message.OperatorID{message.OperatorID(1)}, &message.ConsensusMessage{
				MsgType: message.CommitMsgType,
				Height:  2,
			}),
			SignMsg(t, secretKeys, []message.OperatorID{message.OperatorID(1)}, &message.ConsensusMessage{
				MsgType: message.CommitMsgType,
				Height:  4,
			}),
			true,
			"",
			true,
		},
		{
			"decided from future, requires sync. current is nil",
			nil,
			SignMsg(t, secretKeys, []message.OperatorID{message.OperatorID(1)}, &message.ConsensusMessage{
				MsgType: message.CommitMsgType,
				Height:  2,
			}),
			SignMsg(t, secretKeys, []message.OperatorID{message.OperatorID(1)}, &message.ConsensusMessage{
				MsgType: message.CommitMsgType,
				Height:  4,
			}),
			true,
			"",
			true,
		},
		{
			"decided when init failed to sync",
			nil,
			SignMsg(t, secretKeys, []message.OperatorID{message.OperatorID(1)}, &message.ConsensusMessage{
				MsgType: message.CommitMsgType,
				Height:  1,
			}),
			SignMsg(t, secretKeys, []message.OperatorID{message.OperatorID(1)}, &message.ConsensusMessage{
				MsgType: message.CommitMsgType,
				Height:  1,
			}),
			true,
			"",
			false,
		},
		{
			"decided from far future, requires sync.",
			instance.NewInstanceWithState(&qbft.State{
				Identifier: atomic.Value{},
				Height:     newHeight(3),
			}),
			SignMsg(t, secretKeys, []message.OperatorID{message.OperatorID(1)}, &message.ConsensusMessage{
				MsgType: message.CommitMsgType,
				Height:  2,
			}),
			SignMsg(t, secretKeys, []message.OperatorID{message.OperatorID(1)}, &message.ConsensusMessage{
				MsgType: message.CommitMsgType,
				Height:  10,
			}),
			true,
			"",
			true,
		},
		{
			"decided from past, doesn't requires sync.",
			instance.NewInstanceWithState(&qbft.State{
				Identifier: atomic.Value{},
				Height:     newHeight(3),
			}),
			SignMsg(t, secretKeys, []message.OperatorID{message.OperatorID(1)}, &message.ConsensusMessage{
				MsgType: message.CommitMsgType,
				Height:  2,
			}),
			SignMsg(t, secretKeys, []message.OperatorID{message.OperatorID(1)}, &message.ConsensusMessage{
				MsgType: message.CommitMsgType,
				Height:  1,
			}),
			false,
			"",
			true,
		},
		{
			"decided for current",
			instance.NewInstanceWithState(&qbft.State{
				Identifier: atomic.Value{},
				Height:     newHeight(3),
			}),
			SignMsg(t, secretKeys, []message.OperatorID{message.OperatorID(1)}, &message.ConsensusMessage{
				MsgType: message.CommitMsgType,
				Height:  2,
			}),
			SignMsg(t, secretKeys, []message.OperatorID{message.OperatorID(1)}, &message.ConsensusMessage{
				MsgType: message.CommitMsgType,
				Height:  1,
			}),
			false,
			"",
			true,
		},
		{
			"decided for seq 0",
			instance.NewInstanceWithState(&qbft.State{
				Identifier: atomic.Value{},
				Height:     newHeight(0),
			}),
			nil,
			SignMsg(t, secretKeys, []message.OperatorID{message.OperatorID(1)}, &message.ConsensusMessage{
				MsgType: message.CommitMsgType,
				Height:  0,
			}),
			false,
			"",
			true,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			ctrl := Controller{
				currentInstance: test.currentInstance,
				ibftStorage:     newTestStorage(test.highestDecided),
				initSynced:      atomic.Bool{},
			}
			ctrl.initSynced.Store(test.initSynced)
			res, err := ctrl.decidedRequiresSync(test.msg)
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
	uids := []message.OperatorID{message.OperatorID(1), message.OperatorID(2), message.OperatorID(3), message.OperatorID(4)}
	secretKeys, _ := testing2.GenerateBLSKeys(uids...)
	tests := []struct {
		name            string
		currentInstance instance.Instancer
		msg             *message.SignedMessage
		expectedRes     bool
	}{
		{
			"current instance",
			instance.NewInstanceWithState(&qbft.State{
				Identifier: atomic.Value{},
				Height:     newHeight(1),
			}),
			SignMsg(t, secretKeys, []message.OperatorID{message.OperatorID(1)}, &message.ConsensusMessage{
				MsgType: message.CommitMsgType,
				Height:  1,
			}),
			true,
		},
		{
			"current instance nil",
			&instance.Instance{},
			SignMsg(t, secretKeys, []message.OperatorID{message.OperatorID(1)}, &message.ConsensusMessage{
				MsgType: message.CommitMsgType,
				Height:  1,
			}),
			false,
		},
		{
			"current instance seq lower",
			instance.NewInstanceWithState(&qbft.State{
				Identifier: atomic.Value{},
				Height:     newHeight(1),
			}),
			SignMsg(t, secretKeys, []message.OperatorID{message.OperatorID(1)}, &message.ConsensusMessage{
				MsgType: message.CommitMsgType,
				Height:  2,
			}),
			false,
		},
		{
			"current instance seq higher",
			instance.NewInstanceWithState(&qbft.State{
				Identifier: atomic.Value{},
				Height:     newHeight(4),
			}),
			SignMsg(t, secretKeys, []message.OperatorID{message.OperatorID(1)}, &message.ConsensusMessage{
				MsgType: message.CommitMsgType,
				Height:  2,
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
	uids := []message.OperatorID{message.OperatorID(1), message.OperatorID(2), message.OperatorID(3), message.OperatorID(4)}
	sks, nodes := testing2.GenerateBLSKeys(uids...)
	network := local.NewLocalNetwork()

	identifier := []byte("Identifier_11")
	s1 := populatedStorage(t, sks, 3)
	i1 := populatedIbft(1, identifier, network, s1, sks, nodes, newTestSigner())
	// test before sync
	highest, err := i1.(*Controller).ibftStorage.GetLastDecided(identifier)
	require.NotNil(t, highest)
	require.NoError(t, err)
	require.EqualValues(t, 3, highest.Message.Height)

	time.Sleep(time.Second * 1) // wait for sync to complete

	go func() {
		time.Sleep(time.Millisecond * 500) // wait for instance to start

		signers := []message.OperatorID{message.OperatorID(1), message.OperatorID(2), message.OperatorID(3), message.OperatorID(4)}
		decidedMsg := aggregateSign(t, sks, signers, &message.ConsensusMessage{
			MsgType:    message.CommitMsgType,
			Height:     message.Height(4),
			Round:      message.Round(1),
			Identifier: []byte("value"),
			Data:       []byte("value"),
		})

		i1.(*Controller).ProcessDecidedMessage(decidedMsg)
	}()

	res, err := i1.StartInstance(instance.ControllerStartInstanceOptions{
		Logger:     logex.GetLogger(),
		ValueCheck: &valcheck.AttestationValueCheck{},
		SeqNumber:  4,
		Value:      []byte("value"),
	})
	require.NoError(t, err)
	require.True(t, res.Decided)

	highest, err = i1.(*Controller).ibftStorage.GetLastDecided(identifier)
	require.NotNil(t, highest)
	require.NoError(t, err)
	require.EqualValues(t, 4, highest.Message.Height)
}

func TestSyncAfterDecided(t *testing.T) {
	uids := []message.OperatorID{message.OperatorID(1), message.OperatorID(2), message.OperatorID(3), message.OperatorID(4)}
	sks, nodes := testing2.GenerateBLSKeys(uids...)
	network := local.NewLocalNetwork()

	identifier := []byte("Identifier_11")
	s1 := populatedStorage(t, sks, 4)
	i1 := populatedIbft(1, identifier, network, s1, sks, nodes, newTestSigner())

	_ = populatedIbft(2, identifier, network, populatedStorage(t, sks, 10), sks, nodes, newTestSigner())

	// test before sync
	highest, err := i1.(*Controller).ibftStorage.GetLastDecided(identifier)
	require.NotNil(t, highest)
	require.NoError(t, err)
	require.EqualValues(t, 4, highest.Message.Height)

	decidedMsg := aggregateSign(t, sks, uids, &message.ConsensusMessage{
		MsgType:    message.CommitMsgType,
		Height:     message.Height(10),
		Round:      message.Round(3),
		Identifier: identifier,
		Data:       []byte("value"),
	})

	i1.(*Controller).ProcessDecidedMessage(decidedMsg)

	time.Sleep(time.Millisecond * 500) // wait for sync to complete
	highest, err = i1.(*Controller).ibftStorage.GetLastDecided(identifier)
	require.NotNil(t, highest)
	require.NoError(t, err)
	require.EqualValues(t, 10, highest.Message.Height)
}

func TestSyncFromScratchAfterDecided(t *testing.T) {
	uids := []message.OperatorID{message.OperatorID(1), message.OperatorID(2), message.OperatorID(3), message.OperatorID(4)}
	sks, nodes := testing2.GenerateBLSKeys(uids...)
	network := local.NewLocalNetwork()
	db, _ := kv.New(basedb.Options{
		Type:   "badger-memory",
		Path:   "",
		Logger: zap.L(),
	})

	identifier := []byte("Identifier_11")
	s1 := collections.NewIbft(db, zap.L(), "attestation")
	i1 := populatedIbft(1, identifier, network, &s1, sks, nodes, newTestSigner())

	_ = populatedIbft(2, identifier, network, populatedStorage(t, sks, 10), sks, nodes, newTestSigner())

	decidedMsg := aggregateSign(t, sks, uids, &message.ConsensusMessage{
		MsgType:    message.CommitMsgType,
		Height:     message.Height(10),
		Round:      message.Round(3),
		Identifier: identifier,
		Data:       []byte("value"),
	})

	i1.(*Controller).ProcessDecidedMessage(decidedMsg)

	time.Sleep(time.Millisecond * 500) // wait for sync to complete
	highest, err := i1.(*Controller).ibftStorage.GetLastDecided(identifier)
	require.NotNil(t, highest)
	require.NoError(t, err)
	require.EqualValues(t, 10, highest.Message.Height)
}

func TestValidateDecidedMsg(t *testing.T) {
	uids := []message.OperatorID{message.OperatorID(1), message.OperatorID(2), message.OperatorID(3), message.OperatorID(4)}
	sks, nodes := testing2.GenerateBLSKeys(uids...)
	network := local.NewLocalNetwork()
	identifier := []byte("Identifier_11")
	ibft := populatedIbft(1, identifier, network, populatedStorage(t, sks, 10), sks, nodes, newTestSigner())

	tests := []struct {
		name          string
		msg           *message.SignedMessage
		expectedError error
	}{
		{
			"valid",
			aggregateSign(t, sks, uids, &message.ConsensusMessage{
				MsgType:    message.CommitMsgType,
				Height:     message.Height(11),
				Round:      message.Round(3),
				Identifier: identifier,
				Data:       []byte("value"),
			}),
			nil,
		},
		{
			"invalid msg stage",
			aggregateSign(t, sks, uids, &message.ConsensusMessage{
				MsgType:    message.PrepareMsgType,
				Height:     message.Height(11),
				Round:      message.Round(3),
				Identifier: identifier,
				Data:       []byte("value"),
			}),
			errors.New("message type is wrong"),
		},
		{
			"invalid msg sig",
			aggregateInvalidSign(t, sks, &message.Message{
				Type:       message.RoundState_Commit,
				Round:      3,
				Height:     11,
				Identifier: identifier,
				Value:      []byte("value"),
			}),
			errors.New("could not verify message signature"),
		},
		{
			"valid first decided",
			aggregateSign(t, sks, uids, &message.ConsensusMessage{
				MsgType:    message.CommitMsgType,
				Height:     message.Height(0),
				Round:      message.Round(3),
				Identifier: identifier,
				Data:       []byte("value"),
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

func TestController_checkDecidedMessageSigners(t *testing.T) {
	uids := []message.OperatorID{message.OperatorID(1), message.OperatorID(2), message.OperatorID(3), message.OperatorID(4)}
	secretKeys, nodes := testing2.GenerateBLSKeys(uids...)
	skQuorum := map[uint64]*bls.SecretKey{}
	for i, sk := range secretKeys {
		skQuorum[uint64(i)] = sk
	}
	delete(skQuorum, uint64(4))
	identifier := []byte("Identifier_2")

	incompleteDecided := aggregateSign(t, secretKeys, uids, &message.ConsensusMessage{
		MsgType:    message.CommitMsgType,
		Height:     message.Height(2),
		Identifier: identifier[:],
	})

	completeDecided := aggregateSign(t, secretKeys, uids, &message.ConsensusMessage{
		MsgType:    message.CommitMsgType,
		Height:     message.Height(2),
		Identifier: identifier[:],
	})

	share := &message.Share{
		NodeID:    1,
		PublicKey: validatorPK(secretKeys),
		Committee: nodes,
	}

	ctrl := Controller{
		ValidatorShare: share,
		currentInstance: instance.NewInstanceWithState(&qbft.State{
			Identifier: newIdentifier(identifier),
			Height:     newHeight(2),
		}),
		ibftStorage: newTestStorage(nil),
	}
	require.NoError(t, ctrl.ibftStorage.SaveDecided(incompleteDecided))
	// check message with similar number of signers
	shouldIgnore, err := ctrl.checkDecidedMessageSigners(incompleteDecided)
	require.NoError(t, err)
	require.True(t, shouldIgnore)
	// check message with more signers
	shouldIgnore, err = ctrl.checkDecidedMessageSigners(completeDecided)
	require.NoError(t, err)
	require.False(t, shouldIgnore)
}

func populatedStorage(t *testing.T, sks map[message.OperatorID]*bls.SecretKey, highestSeq int) qbftstorage.QBFTStore {
	storage := newTestStorage(&message.SignedMessage{
		Signature: message.Signature("value"),
		Signers:   nil,
		Message: &message.ConsensusMessage{
			MsgType:    message.CommitMsgType,
			Height:     message.Height(highestSeq),
			Round:      message.Round(3),
			Identifier: []byte("lambda_11"),
			Data:       []byte("value"),
		},
	})
	for i := 0; i <= highestSeq; i++ {
		lambda := []byte("lambda_11")

		signers := []message.OperatorID{message.OperatorID(1), message.OperatorID(2), message.OperatorID(3), message.OperatorID(4)}
		signedMsg := aggregateSign(t, sks, signers, &message.ConsensusMessage{
			MsgType:    message.CommitMsgType,
			Height:     message.Height(i),
			Round:      message.Round(3),
			Identifier: lambda,
			Data:       []byte("value"),
		})
		/*
			aggSignedMsg := aggregateSign(t, sks, &proto.Message{
				Type:      proto.RoundState_Commit,
				Round:     3,
				SeqNumber: uint64(i),
				Lambda:    lambda,
				Value:     []byte("value"),
			})*/
		require.NoError(t, storage.SaveDecided(signedMsg))
		if i == highestSeq {
			require.NoError(t, storage.SaveLastDecided(signedMsg))
		}
	}
	return storage
}

func aggregateSign(t *testing.T, sks map[message.OperatorID]*bls.SecretKey, signers []message.OperatorID, consensusMessage *message.ConsensusMessage) *message.SignedMessage {
	signedMsg := SignMsg(t, sks, signers, consensusMessage)
	require.NoError(t, signedMsg.Aggregate(signedMsg))
	return signedMsg
}

func populatedIbft(
	nodeID uint64,
	identifier []byte,
	network *local.Local,
	ibftStorage collections.Iibft,
	sks map[uint64]*bls.SecretKey,
	nodes map[uint64]*proto.Node,
	signer beaconprotocol.Signer,
) IController {
	share := &storage.Share{
		NodeID:    nodeID,
		PublicKey: validatorPK(sks),
		Committee: nodes,
	}
	ret := New(
		beaconprotocol.RoleTypeAttester,,
		identifier,
		logex.Build("", zap.DebugLevel, nil),
		ibftStorage,
		network.CopyWithLocalNodeID(peer.ID(fmt.Sprintf("%d", nodeID-1))),
		qbft.DefaultConsensusParams(),
		share,
		nil,
		nil,
		signer,
		100*time.Millisecond,
		time.Second*5,
		false)

	ret.(*Controller).setFork(testFork(ret.(*Controller)))
	ret.(*Controller).initHandlers.Set(true) // as if they are already synced
	ret.(*Controller).initSynced.Set(true)   // as if they are already synced
	ret.(*Controller).listenToNetworkMessages()
	ret.(*Controller).listenToSyncMessages()
	ret.(*Controller).processDecidedQueueMessages()
	ret.(*Controller).processSyncQueueMessages()
	return ret
}
