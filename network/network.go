package network

import (
	"context"
	"io"

	"go.uber.org/zap"

	"github.com/bloxapp/ssv/network/commons"
	protocolp2p "github.com/bloxapp/ssv/protocol/v2/p2p"
	"github.com/bloxapp/ssv/protocol/v2/ssv/queue"
)

// MessageRouter is accepting network messages and route them to the corresponding (internal) components
type MessageRouter interface {
	// Route routes the given message, this function MUST NOT block
	Route(ctx context.Context, message *queue.DecodedSSVMessage)
}

// MessageRouting allows to register a MessageRouter
type MessageRouting interface {
	// UseMessageRouter registers a message router to handle incoming messages
	UseMessageRouter(router MessageRouter)
}

// P2PNetwork is a facade interface that provides the entire functionality of the different network interfaces
type P2PNetwork interface {
	io.Closer
	protocolp2p.Network
	MessageRouting
	// Setup initialize the network layer and starts the libp2p host
	Setup(logger *zap.Logger) error
	// Start starts the network
	Start(logger *zap.Logger) error
	// UpdateSubnets will update the registered subnets according to active validators
	UpdateSubnets(logger *zap.Logger)
	// SubscribeAll subscribes to all subnets
	SubscribeAll(logger *zap.Logger) error
	// SubscribeRandoms subscribes to random subnets
	SubscribeRandoms(logger *zap.Logger, numSubnets int) error
}

type ValidatorCounts struct {
	Total     uint32
	Attesting uint32
	Mine      uint32
}

type ValidatorStats struct {
	ValidatorCounts
	Subnets [commons.SubnetsCount]ValidatorCounts
}

// GetValidatorStats returns stats of committees & validators.
type GetValidatorStats func() (ValidatorStats, error)
