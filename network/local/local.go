package local

import (
	"errors"
	"fmt"
	"github.com/bloxapp/ssv/network"
	"github.com/herumi/bls-eth-go-binary/bls"
	"github.com/libp2p/go-libp2p-core/peer"
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
	}
}

// ReceivedMsgChan implements network.Local interface
func (n *Local) ReceivedMsgChan() <-chan *proto.SignedMessage {
	n.createChannelMutex.Lock()
	defer n.createChannelMutex.Unlock()
	c := make(chan *proto.SignedMessage)
	n.msgC = append(n.msgC, c)
	return c
}

// ReceivedSignatureChan returns the channel with signatures
func (n *Local) ReceivedSignatureChan() <-chan *proto.SignedMessage {
	n.createChannelMutex.Lock()
	defer n.createChannelMutex.Unlock()
	c := make(chan *proto.SignedMessage)
	n.sigC = append(n.sigC, c)
	return c
}

// Broadcast implements network.Local interface
func (n *Local) Broadcast(signed *proto.SignedMessage) error {
	go func() {
		for _, c := range n.msgC {
			c <- signed
		}
	}()

	return nil
}

// BroadcastSignature broadcasts the given signature for the given lambda
func (n *Local) BroadcastSignature(msg *proto.SignedMessage) error {
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
func (n *Local) BroadcastDecided(msg *proto.SignedMessage) error {
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
func (n *Local) ReceivedDecidedChan() <-chan *proto.SignedMessage {
	n.createChannelMutex.Lock()
	defer n.createChannelMutex.Unlock()
	c := make(chan *proto.SignedMessage)
	n.decidedC = append(n.decidedC, c)
	return c
}

// GetHighestDecidedInstance sends a highest decided request to peers and returns answers.
// If peer list is nil, broadcasts to all.
func (n *Local) GetHighestDecidedInstance(peerID string, msg *network.SyncMessage) (*network.SyncMessage, error) {
	if toChan, found := n.syncPeers[peerID]; found {
		stream := NewLocalStream(msg.FromPeerID, peerID)
		go func() {
			toChan <- &network.SyncChanObj{
				Msg:    msg,
				Stream: stream,
			}
		}()

		ret := <-stream.ReceiveChan
		return ret, nil
	}
	return nil, errors.New("could not find peer")
}

// RespondToHighestDecidedInstance responds to a GetHighestDecidedInstance
func (n *Local) RespondToHighestDecidedInstance(stream network.SyncStream, msg *network.SyncMessage) error {
	msg.FromPeerID = string(n.localPeerID)
	_, _ = stream.(*Stream).WriteSynMsg(msg)
	return nil
}

// ReceivedSyncMsgChan returns the channel for sync messages
func (n *Local) ReceivedSyncMsgChan() <-chan *network.SyncChanObj {
	n.createChannelMutex.Lock()
	defer n.createChannelMutex.Unlock()
	c := make(chan *network.SyncChanObj)
	n.syncC = append(n.syncC, c)
	n.syncPeers[fmt.Sprintf("%d", len(n.syncPeers))] = c
	return c
}

// GetDecidedByRange returns a list of decided signed messages up to 25 in a batch.
func (n *Local) GetDecidedByRange(peerID string, msg *network.SyncMessage) (*network.SyncMessage, error) {
	if toChan, found := n.syncPeers[peerID]; found {
		stream := NewLocalStream(msg.FromPeerID, peerID)
		go func() {
			toChan <- &network.SyncChanObj{
				Msg:    msg,
				Stream: stream,
			}
		}()

		ret := <-stream.ReceiveChan
		return ret, nil
	}
	return nil, errors.New("could not find peer")
}

// RespondToGetDecidedByRange responds to a GetDecidedByRange
func (n *Local) RespondToGetDecidedByRange(stream network.SyncStream, msg *network.SyncMessage) error {
	msg.FromPeerID = string(n.localPeerID)
	_, _ = stream.(*Stream).WriteSynMsg(msg)
	return nil
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
