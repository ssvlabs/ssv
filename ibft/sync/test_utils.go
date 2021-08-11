package sync

import (
	"bytes"
	"encoding/json"
	"github.com/bloxapp/ssv/ibft/proto"
	"github.com/bloxapp/ssv/network"
	"github.com/bloxapp/ssv/storage/kv"
	"github.com/herumi/bls-eth-go-binary/bls"
	"github.com/pkg/errors"
	"testing"
	"time"
)

type testNetwork struct {
	t                      *testing.T
	highestDecidedReceived map[string]*proto.SignedMessage
	errorsMap              map[string]error
	decidedArr             map[string][]*proto.SignedMessage
	maxBatch               int
	peers                  []string
	retError               error
}

// newTestNetwork returns a new test network instance
func newTestNetwork(
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
