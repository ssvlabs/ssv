package local

import (
	"fmt"
	"github.com/bloxapp/ssv/network"
	"github.com/herumi/bls-eth-go-binary/bls"
	"github.com/libp2p/go-libp2p-core/peer"
	"github.com/pkg/errors"
	"sync"

	"github.com/bloxapp/ssv/ibft/proto"
)

// Local implements network.Local interface
type Local struct {
	localPeerID        peer.ID
	msgC               []chan *proto.SignedMessage
	sigC               []chan *proto.SignedMessage
	decidedC           []chan *proto.SignedMessage
	syncC              []chan *network.SyncChanObj
	syncPeers          map[string]chan *network.SyncChanObj
	createChannelMutex *sync.Mutex
	streamsMut         *sync.Mutex
	streams            map[string]network.SyncStream
}

// NewLocalNetwork creates a new instance of a local network
func NewLocalNetwork() *Local {
	return &Local{
		msgC:               make([]chan *proto.SignedMessage, 0),
		sigC:               make([]chan *proto.SignedMessage, 0),
		decidedC:           make([]chan *proto.SignedMessage, 0),
		syncC:              make([]chan *network.SyncChanObj, 0),
		syncPeers:          make(map[string]chan *network.SyncChanObj),
		createChannelMutex: &sync.Mutex{},
		streamsMut:         &sync.Mutex{},
		streams:            make(map[string]network.SyncStream),
	}
}

// CopyWithLocalNodeID copies the local network instance and adds a unique node id to it
// this is used for peer specific messages like sync messages to identify each node
func (n *Local) CopyWithLocalNodeID(id peer.ID) *Local {
	return &Local{
		localPeerID:        id,
		msgC:               n.msgC,
		sigC:               n.sigC,
		decidedC:           n.decidedC,
		syncC:              n.syncC,
		syncPeers:          n.syncPeers,
		createChannelMutex: n.createChannelMutex,
		streamsMut:         n.streamsMut,
		streams:            n.streams,
	}
}

// ReceivedMsgChan implements network.Local interface
func (n *Local) ReceivedMsgChan() (<-chan *proto.SignedMessage, func()) {
	n.createChannelMutex.Lock()
	defer n.createChannelMutex.Unlock()
	c := make(chan *proto.SignedMessage)
	n.msgC = append(n.msgC, c)
	return c, func() {}
}

// ReceivedSignatureChan returns the channel with signatures
func (n *Local) ReceivedSignatureChan() (<-chan *proto.SignedMessage, func()) {
	n.createChannelMutex.Lock()
	defer n.createChannelMutex.Unlock()
	c := make(chan *proto.SignedMessage)
	n.sigC = append(n.sigC, c)
	return c, func() {}
}

// Broadcast implements network.Local interface
func (n *Local) Broadcast(topicName []byte, signed *proto.SignedMessage) error {
	go func() {
		for _, c := range n.msgC {
			c <- signed
		}
	}()

	return nil
}

// BroadcastSignature broadcasts the given signature for the given lambda
func (n *Local) BroadcastSignature(topicName []byte, msg *proto.SignedMessage) error {
	n.createChannelMutex.Lock()
	go func() {
		for _, c := range n.sigC {
			c <- msg
		}
		n.createChannelMutex.Unlock()
	}()
	return nil
}

// BroadcastDecided broadcasts a decided instance with collected signatures
func (n *Local) BroadcastDecided(topicName []byte, msg *proto.SignedMessage) error {
	n.createChannelMutex.Lock()
	go func() {
		for _, c := range n.decidedC {
			c <- msg
		}
		n.createChannelMutex.Unlock()
	}()
	return nil
}

// ReceivedDecidedChan returns the channel for decided messages
func (n *Local) ReceivedDecidedChan() (<-chan *proto.SignedMessage, func()) {
	n.createChannelMutex.Lock()
	defer n.createChannelMutex.Unlock()
	c := make(chan *proto.SignedMessage)
	n.decidedC = append(n.decidedC, c)
	return c, func() {}
}

// GetHighestDecidedInstance sends a highest decided request to peers and returns answers.
// If peer list is nil, broadcasts to all.
func (n *Local) GetHighestDecidedInstance(peerStr string, msg *network.SyncMessage) (*network.SyncMessage, error) {
	if toChan, found := n.syncPeers[peerStr]; found {
		stream := NewLocalStream(msg.FromPeerID, peerStr)
		go func() {
			toChan <- &network.SyncChanObj{
				Msg:      msg,
				StreamID: stream.ID(),
			}
		}()
		n.addStream(stream)
		ret := <-stream.(*Stream).ReceiveChan
		return ret, nil
	}
	return nil, errors.New("could not find peer")
}

// RespondSyncMsg responds to sync messages
func (n *Local) RespondSyncMsg(streamID string, msg *network.SyncMessage) error {
	msg.FromPeerID = string(n.localPeerID)
	n.streamsMut.Lock()
	stream, ok := n.streams[streamID]
	delete(n.streams, streamID)
	n.streamsMut.Unlock()
	if !ok {
		return errors.New("stream not found")
	}
	_, _ = stream.(*Stream).WriteSynMsg(msg)
	return nil
}

// addStream save a reference to the stream
func (n *Local) addStream(stream network.SyncStream) {
	n.streamsMut.Lock()
	n.streams[stream.ID()] = stream
	n.streamsMut.Unlock()
}

// ReceivedSyncMsgChan returns the channel for sync messages
func (n *Local) ReceivedSyncMsgChan() (<-chan *network.SyncChanObj, func()) {
	n.createChannelMutex.Lock()
	defer n.createChannelMutex.Unlock()
	c := make(chan *network.SyncChanObj)
	n.syncC = append(n.syncC, c)
	n.syncPeers[fmt.Sprintf("%d", len(n.syncPeers))] = c
	return c, func() {}
}

// GetDecidedByRange returns a list of decided signed messages up to 25 in a batch.
func (n *Local) GetDecidedByRange(peerStr string, msg *network.SyncMessage) (*network.SyncMessage, error) {
	if toChan, found := n.syncPeers[peerStr]; found {
		stream := NewLocalStream(msg.FromPeerID, peerStr)
		go func() {
			toChan <- &network.SyncChanObj{
				Msg:      msg,
				StreamID: stream.ID(),
			}
		}()
		n.addStream(stream)
		ret := <-stream.(*Stream).ReceiveChan
		return ret, nil
	}
	return nil, errors.New("could not find peer")
}

// SubscribeToValidatorNetwork  for new validator create new topic, subscribe and start listen
func (n *Local) SubscribeToValidatorNetwork(validatorPk *bls.PublicKey) error {
	return nil
}

// AllPeers returns all connected peers for a validator PK
func (n *Local) AllPeers(validatorPk []byte) ([]string, error) {
	ret := make([]string, 0)
	for k := range n.syncPeers {
		ret = append(ret, k)
	}
	return ret, nil
}

// MaxBatch implementation
func (n *Local) MaxBatch() uint64 {
	return 25
}

// GetLastChangeRoundMsg returns the latest change round msg for a running instance, could return nil
func (n *Local) GetLastChangeRoundMsg(peerStr string, msg *network.SyncMessage) (*network.SyncMessage, error) {
	return nil, nil
}

// SubscribeToMainTopic implementation
func (n *Local) SubscribeToMainTopic() error {
	return nil
}

// NotifyOperatorID implementation
func (n *Local) NotifyOperatorID(oid string) {
}
