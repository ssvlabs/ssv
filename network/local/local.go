package local

import (
	"github.com/bloxapp/ssv/network"
	"github.com/herumi/bls-eth-go-binary/bls"
	"github.com/libp2p/go-libp2p-core/peer"
	"sync"

	"github.com/bloxapp/ssv/ibft/proto"
)

// Local implements network.Local interface
type Local struct {
	msgC               []chan *proto.SignedMessage
	sigC               []chan *proto.SignedMessage
	decidedC           []chan *proto.SignedMessage
	syncC              []chan *network.SyncChanObj
	createChannelMutex sync.Mutex
}

// NewLocalNetwork creates a new instance of a local network
func NewLocalNetwork() *Local {
	return &Local{
		msgC:     make([]chan *proto.SignedMessage, 0),
		sigC:     make([]chan *proto.SignedMessage, 0),
		decidedC: make([]chan *proto.SignedMessage, 0),
		syncC:    make([]chan *network.SyncChanObj, 0),
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

// BroadcastSyncMessage broadcasts a sync message to peers.
// If peer list is nil, broadcasts to all.
func (n *Local) BroadcastSyncMessage(peers []peer.ID, msg *network.SyncMessage) error {
	//n.createChannelMutex.Lock()
	//go func() {
	//	for _, c := range n.syncC {
	//		c <- msg
	//	}
	//	n.createChannelMutex.Unlock()
	//}()
	//return nil
	panic("implement")
}

// GetHighestDecidedInstance sends a highest decided request to peers and returns answers.
// If peer list is nil, broadcasts to all.
func (n *Local) GetHighestDecidedInstance(peer peer.ID, msg *network.SyncMessage) (*network.SyncMessage, error) {
	panic("implement")
}

// RespondToHighestDecidedInstance responds to a GetHighestDecidedInstance
func (n *Local) RespondToHighestDecidedInstance(stream network.SyncStream, msg *network.SyncMessage) error {
	panic("implement")
}

// ReceivedSyncMsgChan returns the channel for sync messages
func (n *Local) ReceivedSyncMsgChan() <-chan *network.SyncChanObj {
	n.createChannelMutex.Lock()
	defer n.createChannelMutex.Unlock()
	c := make(chan *network.SyncChanObj)
	n.syncC = append(n.syncC, c)
	return c
}

// GetDecidedByRange returns a list of decided signed messages up to 25 in a batch.
func (n *Local) GetDecidedByRange(peer peer.ID, msg *network.SyncMessage) (*network.SyncMessage, error) {
	panic("implement")
}

// RespondToGetDecidedByRange responds to a GetDecidedByRange
func (n *Local) RespondToGetDecidedByRange(stream network.SyncStream, msg *network.SyncMessage) error {
	panic("implement")
}

// SubscribeToValidatorNetwork  for new validator create new topic, subscribe and start listen
func (n *Local) SubscribeToValidatorNetwork(validatorPk *bls.PublicKey) error {
	return nil
}

// AllPeers returns all connected peers for a validator PK
func (n *Local) AllPeers(validatorPk []byte) ([]peer.ID, error) {
	panic("implement")
}
