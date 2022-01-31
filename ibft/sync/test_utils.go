package sync

import (
	"bytes"
	"encoding/json"
	"fmt"
	"github.com/bloxapp/ssv/ibft/proto"
	"github.com/bloxapp/ssv/network"
	ssvstorage "github.com/bloxapp/ssv/storage"
	"github.com/bloxapp/ssv/storage/basedb"
	"github.com/bloxapp/ssv/storage/collections"
	"github.com/bloxapp/ssv/storage/kv"
	"github.com/herumi/bls-eth-go-binary/bls"
	"github.com/pkg/errors"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"sync/atomic"
	"testing"
	"time"
)

// TestingIbftStorage returns a testing storage
func TestingIbftStorage(t *testing.T) collections.IbftStorage {
	db, err := ssvstorage.GetStorageFactory(basedb.Options{
		Type:   "badger-memory",
		Logger: zap.L(),
		Path:   "",
	})
	require.NoError(t, err)
	return collections.NewIbft(db, zap.L(), "attestation")
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

// DecidedArr returns an array of signed decided msgs for the given identifier
func DecidedArr(t *testing.T, maxSeq uint64, sks map[uint64]*bls.SecretKey, identifier []byte) []*proto.SignedMessage {
	ret := make([]*proto.SignedMessage, 0)
	for i := uint64(0); i <= maxSeq; i++ {
		ret = append(ret, MultiSignMsg(t, []uint64{1, 2, 3}, sks, &proto.Message{
			Type:      proto.RoundState_Decided,
			Round:     1,
			Lambda:    identifier[:],
			SeqNumber: i,
		}))
	}
	return ret
}

// MultiSignMsg signs a msg with multiple signers
func MultiSignMsg(t *testing.T, ids []uint64, sks map[uint64]*bls.SecretKey, msg *proto.Message) *proto.SignedMessage {
	require.NoError(t, bls.Init(bls.BLS12_381))

	var agg *bls.Sign
	for _, id := range ids {
		signature, err := msg.Sign(sks[id])
		require.NoError(t, err)
		if agg == nil {
			agg = signature
		} else {
			agg.Add(signature)
		}
	}

	return &proto.SignedMessage{
		Message:   msg,
		Signature: agg.Serialize(),
		SignerIds: ids,
	}
}

// TestNetwork struct
type TestNetwork struct {
	t                      *testing.T
	highestDecidedReceived map[string]*proto.SignedMessage
	errorsMap              map[string]error
	decidedArr             map[string][]*proto.SignedMessage
	lastMsgs               map[string]*proto.SignedMessage
	maxBatch               int
	peers                  []string
	retError               error
	streamProvider         func(string) network.SyncStream
}

// NewTestNetwork returns a new test network instance
func NewTestNetwork(
	t *testing.T, peers []string,
	maxBatch int,
	highestDecidedReceived map[string]*proto.SignedMessage,
	errorsMap map[string]error,
	decidedArr map[string][]*proto.SignedMessage,
	lastMsgs map[string]*proto.SignedMessage,
	retError error,
	streamProvider func(string) network.SyncStream,
) *TestNetwork {
	return &TestNetwork{
		t:                      t,
		peers:                  peers,
		maxBatch:               maxBatch,
		highestDecidedReceived: highestDecidedReceived,
		errorsMap:              errorsMap,
		decidedArr:             decidedArr,
		lastMsgs:               lastMsgs,
		retError:               retError,
		streamProvider:         streamProvider,
	}
}

// Broadcast impl
func (n *TestNetwork) Broadcast(topicName []byte, msg *proto.SignedMessage) error {
	return nil
}

// ReceivedMsgChan impl
func (n *TestNetwork) ReceivedMsgChan() (<-chan *proto.SignedMessage, func()) {
	return nil, func() {}
}

// BroadcastSignature impl
func (n *TestNetwork) BroadcastSignature(topicName []byte, msg *proto.SignedMessage) error {
	return nil
}

// ReceivedSignatureChan impl
func (n *TestNetwork) ReceivedSignatureChan() (<-chan *proto.SignedMessage, func()) {
	return nil, func() {}
}

// BroadcastDecided impl
func (n *TestNetwork) BroadcastDecided(topicName []byte, msg *proto.SignedMessage) error {
	return nil
}

// ReceivedDecidedChan impl
func (n *TestNetwork) ReceivedDecidedChan() (<-chan *proto.SignedMessage, func()) {
	return nil, func() {}
}

// GetHighestDecidedInstance impl
func (n *TestNetwork) GetHighestDecidedInstance(peerStr string, msg *network.SyncMessage) (*network.SyncMessage, error) {
	time.Sleep(time.Millisecond * 100)

	if err, found := n.errorsMap[peerStr]; found {
		return nil, err
	}

	if highest, found := n.highestDecidedReceived[peerStr]; found {
		if highest == nil {
			// as if no highest.
			return &network.SyncMessage{
				Error:      kv.EntryNotFoundError,
				FromPeerID: peerStr,
				Type:       network.Sync_GetInstanceRange,
			}, nil
		}

		if !bytes.Equal(msg.Lambda, highest.Message.Lambda) {
			return nil, errors.New("could not find highest")
		}

		return &network.SyncMessage{
			SignedMessages: []*proto.SignedMessage{highest},
			FromPeerID:     peerStr,
			Type:           network.Sync_GetInstanceRange,
		}, nil
	}
	return nil, errors.New("could not find highest")
}

// RespondSyncMsg implementation
func (n *TestNetwork) RespondSyncMsg(streamID string, msg *network.SyncMessage) error {
	msgBytes, err := json.Marshal(network.Message{
		SyncMessage: msg,
		Type:        network.NetworkMsg_SyncType,
	})
	if err != nil {
		return errors.Wrap(err, "failed to marshal message")
	}
	return n.streamProvider(streamID).WriteWithTimeout(msgBytes, time.Second*5)
}

// GetDecidedByRange implementation
func (n *TestNetwork) GetDecidedByRange(peerStr string, msg *network.SyncMessage) (*network.SyncMessage, error) {
	time.Sleep(time.Millisecond * 100)

	if n.retError != nil {
		return nil, n.retError
	}

	if arr, found := n.decidedArr[peerStr]; found {
		if !bytes.Equal(msg.Lambda, arr[0].Message.Lambda) {
			return nil, errors.New("could not find highest")
		}

		ret := make([]*proto.SignedMessage, 0)
		for _, m := range arr {
			if m.Message.SeqNumber >= msg.Params[0] && m.Message.SeqNumber <= msg.Params[1] {
				ret = append(ret, m)
			}
			if len(ret) == n.maxBatch {
				break
			}
		}

		return &network.SyncMessage{
			SignedMessages: ret,
			FromPeerID:     peerStr,
			Lambda:         msg.Lambda,
			Type:           network.Sync_GetInstanceRange,
		}, nil
	}
	return nil, errors.New("could not find highest")
}

// GetLastChangeRoundMsg returns the latest change round msg for a running instance, could return nil
func (n *TestNetwork) GetLastChangeRoundMsg(peerStr string, msg *network.SyncMessage) (*network.SyncMessage, error) {
	if last, found := n.lastMsgs[peerStr]; found {
		if last == nil {
			// as if no highest.
			return &network.SyncMessage{
				Error:      kv.EntryNotFoundError,
				FromPeerID: peerStr,
				Type:       network.Sync_GetLatestChangeRound,
			}, nil
		}

		if !bytes.Equal(msg.Lambda, last.Message.Lambda) {
			return nil, errors.New("could not find highest")
		}

		return &network.SyncMessage{
			SignedMessages: []*proto.SignedMessage{last},
			FromPeerID:     peerStr,
			Type:           network.Sync_GetInstanceRange,
		}, nil
	}
	return nil, errors.New("could not find highest")
}

// ReceivedSyncMsgChan implementation
func (n *TestNetwork) ReceivedSyncMsgChan() (<-chan *network.SyncChanObj, func()) {
	return nil, func() {}
}

// SubscribeToValidatorNetwork subscribing and listen to validator network
func (n *TestNetwork) SubscribeToValidatorNetwork(validatorPk *bls.PublicKey) error {
	return nil
}

// AllPeers returns all connected peers for a validator PK
func (n *TestNetwork) AllPeers(validatorPk []byte) ([]string, error) {
	return n.peers, nil
}

// MaxBatch implementation
func (n *TestNetwork) MaxBatch() uint64 {
	return uint64(n.maxBatch)
}

// SubscribeToMainTopic implementation
func (n *TestNetwork) SubscribeToMainTopic() error {
	return nil
}

// NotifyOperatorID implementation
func (n *TestNetwork) NotifyOperatorID(oid string) {
}

var testStreamCounter int64

// TestStream struct
type TestStream struct {
	C    chan []byte
	peer string
	id   string
}

// NewTestStream returns a new instance of test stream
func NewTestStream(remotePeer string) *TestStream {
	return &TestStream{
		peer: remotePeer,
		C:    make(chan []byte),
		id:   fmt.Sprintf("id-%d", atomic.AddInt64(&testStreamCounter, 1)),
	}
}

// ID implementation
func (s *TestStream) ID() string {
	return s.id
}

// Close implementation
func (s *TestStream) Close() error {
	return nil
}

// CloseWrite implementation
func (s *TestStream) CloseWrite() error {
	return nil
}

// RemotePeer implementation
func (s *TestStream) RemotePeer() string {
	return s.peer
}

// ReadWithTimeout will read bytes from stream and return the result, will return error if timeout or error.
// does not close stream when returns
func (s *TestStream) ReadWithTimeout(timeout time.Duration) ([]byte, error) {
	return nil, nil
}

// WriteWithTimeout will write bytes to stream, will return error if timeout or error.
// does not close stream when returns
func (s *TestStream) WriteWithTimeout(data []byte, timeout time.Duration) error {
	go func() {
		time.After(time.Millisecond * 100)
		s.C <- data
	}()
	return nil
}
