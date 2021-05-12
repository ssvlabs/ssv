package sync

import (
	"bytes"
	"errors"
	"github.com/bloxapp/ssv/ibft/proto"
	"github.com/bloxapp/ssv/network"
	"github.com/herumi/bls-eth-go-binary/bls"
	"github.com/libp2p/go-libp2p-core/peer"
	"testing"
	"time"
)

type testNetwork struct {
	t                      *testing.T
	highestDecidedReceived map[peer.ID]*proto.SignedMessage
	peers                  []peer.ID
}

func NewTestNetwork(t *testing.T, peers []peer.ID, highestDecidedReceived map[peer.ID]*proto.SignedMessage) *testNetwork {
	return &testNetwork{t: t, peers: peers, highestDecidedReceived: highestDecidedReceived}
}

func (n *testNetwork) Broadcast(msg *proto.SignedMessage) error {
	return nil
}

func (n *testNetwork) ReceivedMsgChan() <-chan *proto.SignedMessage {
	return nil
}

func (n *testNetwork) BroadcastSignature(msg *proto.SignedMessage) error {
	return nil
}

func (n *testNetwork) ReceivedSignatureChan() <-chan *proto.SignedMessage {
	return nil
}

func (n *testNetwork) BroadcastDecided(msg *proto.SignedMessage) error {
	return nil
}

func (n *testNetwork) ReceivedDecidedChan() <-chan *proto.SignedMessage {
	return nil
}

func (n *testNetwork) GetHighestDecidedInstance(peer peer.ID, msg *network.SyncMessage) (*network.Message, error) {
	time.Sleep(time.Millisecond * 100)

	if highest, found := n.highestDecidedReceived[peer]; found {
		if !bytes.Equal(msg.ValidatorPk, highest.Message.ValidatorPk) {
			return nil, errors.New("could not find highest")
		}

		return &network.Message{
			SignedMessage: highest,
			Type:          network.NetworkMsg_SyncType,
		}, nil
	}
	return nil, errors.New("could not find highest")
}

func (n *testNetwork) RespondToHighestDecidedInstance(stream network.SyncStream, msg *network.SyncMessage) error {
	return nil
}

func (n *testNetwork) ReceivedSyncMsgChan() <-chan *network.SyncChanObj {
	return nil
}

// SubscribeToValidatorNetwork subscribing and listen to validator network
func (s *testNetwork) SubscribeToValidatorNetwork(validatorPk *bls.PublicKey) error {
	return nil
}

// AllPeers returns all connected peers for a validator PK
func (s *testNetwork) AllPeers(validatorPk []byte) ([]peer.ID, error) {
	return s.peers, nil
}

//type testStorage struct {
//	highestDecided *proto.SignedMessage
//}
//
//func NewTestStorage(highestDecided *proto.SignedMessage) *testStorage {
//	return &testStorage{highestDecided: highestDecided}
//}
//
//func (s *testStorage) SaveCurrentInstance(state *proto.State) error {
//	return nil
//}
//
//func (s *testStorage) GetCurrentInstance(pk []byte) (*proto.State, error) {
//	return nil, nil
//}
//
//func (s *testStorage) SaveDecided(signedMsg *proto.SignedMessage) error {
//	return nil
//}
//
//func (s *testStorage) GetDecided(pk []byte, seqNumber uint64) (*proto.SignedMessage, error) {
//	return nil, nil
//}
//
//func (s *testStorage) SaveHighestDecidedInstance(signedMsg *proto.SignedMessage) error {
//	return nil
//}
//
//func (s *testStorage) GetHighestDecidedInstance(pk []byte) (*proto.SignedMessage, error) {
//	return s.highestDecided, nil
//}
