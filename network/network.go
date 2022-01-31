package network

import (
	"github.com/bloxapp/ssv/ibft/proto"
	"github.com/herumi/bls-eth-go-binary/bls"
	"io"
	"time"
)

// Message is a container for network messages.
type Message struct {
	SignedMessage *proto.SignedMessage
	SyncMessage   *SyncMessage
	Type          NetworkMsg
	StreamID      string
}

// SyncChanObj is a wrapper object for streaming of sync messages
type SyncChanObj struct {
	Msg      *SyncMessage
	StreamID string
}

// SyncStream is a interface for all stream related functions for the sync process.
type SyncStream interface {
	io.Closer

	// CloseWrite closes the stream for writing but leaves it open for
	// reading.
	//
	// CloseWrite does not free the stream, users must still call Close or
	// Reset.
	CloseWrite() error

	// RemotePeer returns a string identifier of the remote peer connected to this stream
	RemotePeer() string

	// ReadWithTimeout will read bytes from stream and return the result, will return error if timeout or error.
	// does not close stream when returns
	ReadWithTimeout(timeout time.Duration) ([]byte, error)

	// WriteWithTimeout will write bytes to stream, will return error if timeout or error.
	// does not close stream when returns
	WriteWithTimeout(data []byte, timeout time.Duration) error

	// ID returns the id of the stream
	ID() string
}

// Reader is the interface for reading messages from the network
type Reader interface {
	// ReceivedMsgChan is a channel that forwards new propagated messages to a subscriber
	ReceivedMsgChan() (<-chan *proto.SignedMessage, func())
	// ReceivedSignatureChan returns the channel with signatures
	ReceivedSignatureChan() (<-chan *proto.SignedMessage, func())
	// ReceivedDecidedChan returns the channel for decided messages
	ReceivedDecidedChan() (<-chan *proto.SignedMessage, func())
	// ReceivedSyncMsgChan returns the channel for sync messages
	ReceivedSyncMsgChan() (<-chan *SyncChanObj, func())
	// SubscribeToValidatorNetwork subscribes and listens to validator's network
	SubscribeToValidatorNetwork(validatorPk *bls.PublicKey) error
	// AllPeers returns all connected peers for a validator PK
	AllPeers(validatorPk []byte) ([]string, error)
	// SubscribeToMainTopic subscribes to main topic
	SubscribeToMainTopic() error
	// MaxBatch returns the maximum batch size for network responses
	MaxBatch() uint64
}

// Broadcaster is the interface for broadcasting messages in the network
type Broadcaster interface {
	// Broadcast propagates a signed message to all peers
	Broadcast(topicName []byte, msg *proto.SignedMessage) error
	// BroadcastSignature broadcasts the given signature for the given lambda
	BroadcastSignature(topicName []byte, msg *proto.SignedMessage) error
	// BroadcastDecided broadcasts a decided instance with collected signatures
	BroadcastDecided(topicName []byte, msg *proto.SignedMessage) error
	// MaxBatch returns the maximum batch size for network responses
	MaxBatch() uint64
}

// Syncer represents the needed functionality for performing sync
type Syncer interface {
	// GetHighestDecidedInstance sends a highest decided request to peers and returns answers.
	// If peer list is nil, broadcasts to all.
	GetHighestDecidedInstance(peerStr string, msg *SyncMessage) (*SyncMessage, error)
	// GetDecidedByRange returns a list of decided signed messages up to 25 in a batch.
	GetDecidedByRange(peerStr string, msg *SyncMessage) (*SyncMessage, error)
	// GetLastChangeRoundMsg returns the latest change round msg for a running instance, could return nil
	GetLastChangeRoundMsg(peerStr string, msg *SyncMessage) (*SyncMessage, error)
	// RespondSyncMsg responds to the stream with the given message
	RespondSyncMsg(streamID string, msg *SyncMessage) error
}

// Network represents the behavior of the network
type Network interface {
	Reader
	Broadcaster
	Syncer

	// NotifyOperatorID updates the network regarding new operators joining the network
	// TODO: find a better way to do this
	NotifyOperatorID(oid string)
}
