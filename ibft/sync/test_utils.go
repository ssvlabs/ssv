package sync

import (
	"bytes"
	"encoding/json"
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
	"testing"
	"time"
)

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

func DecidedArr(t *testing.T, maxSeq uint64, sks map[uint64]*bls.SecretKey) []*proto.SignedMessage {
	ret := make([]*proto.SignedMessage, 0)
	for i := uint64(0); i <= maxSeq; i++ {
		ret = append(ret, MultiSignMsg(t, []uint64{1, 2, 3}, sks, &proto.Message{
			Type:      proto.RoundState_Decided,
			Round:     1,
			Lambda:    []byte("lambda"),
			SeqNumber: i,
		}))
	}
	return ret
}

func MultiSignMsg(t *testing.T, ids []uint64, sks map[uint64]*bls.SecretKey, msg *proto.Message) *proto.SignedMessage {
	bls.Init(bls.BLS12_381)

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

func TestingIbftStorage(t *testing.T) collections.IbftStorage {
	db, err := ssvstorage.GetStorageFactory(basedb.Options{
		Type:   "badger-memory",
		Logger: zap.L(),
		Path:   "",
	})
	require.NoError(t, err)
	return collections.NewIbft(db, zap.L(), "attestation")
}

type testNetwork struct {
	t                      *testing.T
	highestDecidedReceived map[string]*proto.SignedMessage
	errorsMap              map[string]error
	decidedArr             map[string][]*proto.SignedMessage
	maxBatch               int
	peers                  []string
	retError               error
}

// NewTestNetwork returns a new test network instance
func NewTestNetwork(
	t *testing.T, peers []string,
	maxBatch int,
	highestDecidedReceived map[string]*proto.SignedMessage,
	errorsMap map[string]error,
	decidedArr map[string][]*proto.SignedMessage,
	retError error,
) *testNetwork {
	return &testNetwork{
		t:                      t,
		peers:                  peers,
		maxBatch:               maxBatch,
		highestDecidedReceived: highestDecidedReceived,
		errorsMap:              errorsMap,
		decidedArr:             decidedArr,
		retError:               retError,
	}
}

func (n *testNetwork) Broadcast(topicName []byte, msg *proto.SignedMessage) error {
	return nil
}

func (n *testNetwork) ReceivedMsgChan() <-chan *proto.SignedMessage {
	return nil
}

func (n *testNetwork) BroadcastSignature(topicName []byte, msg *proto.SignedMessage) error {
	return nil
}

func (n *testNetwork) ReceivedSignatureChan() <-chan *proto.SignedMessage {
	return nil
}

func (n *testNetwork) BroadcastDecided(topicName []byte, msg *proto.SignedMessage) error {
	return nil
}

func (n *testNetwork) ReceivedDecidedChan() <-chan *proto.SignedMessage {
	return nil
}

func (n *testNetwork) GetHighestDecidedInstance(peerStr string, msg *network.SyncMessage) (*network.SyncMessage, error) {
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

func (n *testNetwork) RespondToHighestDecidedInstance(stream network.SyncStream, msg *network.SyncMessage) error {
	return nil
}

func (n *testNetwork) GetDecidedByRange(peerStr string, msg *network.SyncMessage) (*network.SyncMessage, error) {
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

// RespondToGetDecidedByRange responds to a GetDecidedByRange
func (n *testNetwork) RespondToGetDecidedByRange(stream network.SyncStream, msg *network.SyncMessage) error {
	msgBytes, err := json.Marshal(network.Message{
		SyncMessage: msg,
		Type:        network.NetworkMsg_SyncType,
	})
	if err != nil {
		return errors.Wrap(err, "failed to marshal message")
	}
	_, err = stream.Write(msgBytes)
	return err
}

// GetCurrentInstance returns the latest msg sent from a running instance
func (n *testNetwork) GetCurrentInstance(peerStr string, msg *network.SyncMessage) (*network.SyncMessage, error) {
	return nil, nil
}

// RespondToGetCurrentInstance responds to a GetCurrentInstance
func (n *testNetwork) RespondToGetCurrentInstance(stream network.SyncStream, msg *network.SyncMessage) error {
	return nil
}

func (n *testNetwork) ReceivedSyncMsgChan() <-chan *network.SyncChanObj {
	return nil
}

// SubscribeToValidatorNetwork subscribing and listen to validator network
func (n *testNetwork) SubscribeToValidatorNetwork(validatorPk *bls.PublicKey) error {
	return nil
}

// IsSubscribeToValidatorNetwork checks if there is a subscription to the validator topic
func (n *testNetwork) IsSubscribeToValidatorNetwork(validatorPk *bls.PublicKey) bool {
	return false
}

// AllPeers returns all connected peers for a validator PK
func (n *testNetwork) AllPeers(validatorPk []byte) ([]string, error) {
	return n.peers, nil
}

// MaxBatch returns max batch size
func (n *testNetwork) MaxBatch() uint64 {
	return uint64(n.maxBatch)
}

type testStream struct {
	C    chan []byte
	peer string
}

// NewTestStream returns a new instance of test stream
func NewTestStream(remotePeer string) *testStream {
	return &testStream{
		peer: remotePeer,
		C:    make(chan []byte),
	}
}

func (s *testStream) Read(p []byte) (n int, err error) {
	return 0, nil
}

func (s *testStream) Write(p []byte) (n int, err error) {
	go func() {
		time.After(time.Millisecond * 100)
		s.C <- p
	}()

	return 0, nil
}

func (s *testStream) Close() error {
	return nil
}

func (s *testStream) CloseWrite() error {
	return nil
}

func (s *testStream) RemotePeer() string {
	return s.peer
}
