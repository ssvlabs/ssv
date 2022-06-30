package forks

import (
	"github.com/bloxapp/ssv/protocol/v1/message"
	p2pprotocol "github.com/bloxapp/ssv/protocol/v1/p2p"
	"github.com/ethereum/go-ethereum/p2p/enode"
	"github.com/libp2p/go-libp2p"
	"github.com/libp2p/go-libp2p-core/protocol"
)

// Fork is an interface for network specific fork implementations
type Fork interface {
	encoding
	pubSubMapping
	pubSubConfig
	sync
	nodeRecord
	libp2pConfig
}

type nodeRecord interface {
	// DecorateNode will enrich the local node record with more entries, according to current fork
	DecorateNode(node *enode.LocalNode, args map[string]interface{}) error
}

type pubSubMapping interface {
	// ValidatorTopicID maps the given validator public key to the corresponding pubsub topic
	ValidatorTopicID(pk []byte) []string
	// DecidedTopic returns the name used for main/decided topic,
	// or empty string if the fork doesn't include a decided topic
	DecidedTopic() string
	// GetTopicFullName returns the topic full name, including prefix
	GetTopicFullName(baseName string) string
	// GetTopicBaseName return the base topic name of the topic, w/o ssv prefix
	GetTopicBaseName(topicName string) string
	// ValidatorSubnet returns the subnet for the given validator
	ValidatorSubnet(validatorPKHex string) int
}

type pubSubConfig interface {
	// MsgID is the msgID function to use for pubsub
	MsgID() MsgIDFunc
	// Subnets returns the subnets count for this fork
	Subnets() int
}

type libp2pConfig interface {
	// AddOptions enables to inject libp2p options according to the given fork
	AddOptions(opts []libp2p.Option) []libp2p.Option
}

type encoding interface {
	// EncodeNetworkMsg encodes the given message
	EncodeNetworkMsg(msg *message.SSVMessage) ([]byte, error)
	// DecodeNetworkMsg decodes the given message
	DecodeNetworkMsg(data []byte) (*message.SSVMessage, error)
}

type sync interface {
	// ProtocolID returns the protocol id of given protocol,
	// and the amount of peers for distribution
	ProtocolID(prot p2pprotocol.SyncProtocol) (protocol.ID, int)
}

// MsgIDFunc is the function that maps a message to a msg_id
type MsgIDFunc func(msg []byte) string
