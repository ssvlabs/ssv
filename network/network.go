package network

import (
	"github.com/bloxapp/ssv/ibft/proto"
	"github.com/herumi/bls-eth-go-binary/bls"
	"io"
)

// Message is a container for network messages.
type Message struct {
	SignedMessage *proto.SignedMessage
	SyncMessage   *SyncMessage
	Stream        SyncStream
	Type          NetworkMsg
}

// SyncChanObj is a wrapper object for streaming of sync messages
type SyncChanObj struct {
	Msg    *SyncMessage
	Stream SyncStream
}

// SyncStream is a interface for all stream related functions for the sync process.
type SyncStream interface {
	io.Reader
	io.Writer
	io.Closer

	// CloseWrite closes the stream for writing but leaves it open for
	// reading.
	//
	// CloseWrite does not free the stream, users must still call Close or
	// Reset.
	CloseWrite() error

	// RemotePeer returns a string identifier of the remote peer connected to this stream
	RemotePeer() string
}

// Network represents the behavior of the network
type Network interface {
	// Broadcast propagates a signed message to all peers
	Broadcast(topicName []byte, msg *proto.SignedMessage) error

	// ReceivedMsgChan is a channel that forwards new propagated messages to a subscriber
	ReceivedMsgChan() <-chan *proto.SignedMessage

	// BroadcastSignature broadcasts the given signature for the given lambda
	BroadcastSignature(topicName []byte, msg *proto.SignedMessage) error

	// ReceivedSignatureChan returns the channel with signatures
	ReceivedSignatureChan() <-chan *proto.SignedMessage

	// BroadcastDecided broadcasts a decided instance with collected signatures
	BroadcastDecided(topicName []byte, msg *proto.SignedMessage) error

	// ReceivedDecidedChan returns the channel for decided messages
	ReceivedDecidedChan() <-chan *proto.SignedMessage

	// GetHighestDecidedInstance sends a highest decided request to peers and returns answers.
	// If peer list is nil, broadcasts to all.
	GetHighestDecidedInstance(peerStr string, msg *SyncMessage) (*SyncMessage, error)

	// RespondToHighestDecidedInstance responds to a GetHighestDecidedInstance
	RespondToHighestDecidedInstance(stream SyncStream, msg *SyncMessage) error

	// GetDecidedByRange returns a list of decided signed messages up to 25 in a batch.
	GetDecidedByRange(peerStr string, msg *SyncMessage) (*SyncMessage, error)

	// RespondToGetDecidedByRange responds to a GetDecidedByRange
	RespondToGetDecidedByRange(stream SyncStream, msg *SyncMessage) error

	// GetCurrentInstance returns the latest msg sent from a running instance
	GetCurrentInstance(peerStr string, msg *SyncMessage) (*SyncMessage, error)

	// RespondToGetCurrentInstance responds to a GetCurrentInstance
	RespondToGetCurrentInstance(stream SyncStream, msg *SyncMessage) error

	// ReceivedSyncMsgChan returns the channel for sync messages
	ReceivedSyncMsgChan() <-chan *SyncChanObj

	// SubscribeToValidatorNetwork subscribing and listen to validator network
	SubscribeToValidatorNetwork(validatorPk *bls.PublicKey) error

	// IsSubscribeToValidatorNetwork checks if there is a subscription to the validator topic
	IsSubscribeToValidatorNetwork(validatorPk *bls.PublicKey) bool

	// AllPeers returns all connected peers for a validator PK
	AllPeers(validatorPk []byte) ([]string, error)

	MaxBatch() uint64
}
