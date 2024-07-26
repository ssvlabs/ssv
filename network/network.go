package network

import (
	"context"
	"github.com/libp2p/go-libp2p/core/peer"
	"io"

	"go.uber.org/zap"

	protocolp2p "github.com/ssvlabs/ssv/protocol/v2/p2p"
)

// SSVMessageInterface serves as a marker interface for any SSV message types.
type SSVMessageInterface interface {
	// Potentially no methods needed if you're using only type assertions
}

// MessageRouter is accepting network messages and route them to the corresponding (internal) components
type MessageRouter interface {
	// Route routes the given message, this function MUST NOT block
	Route(ctx context.Context, message SSVMessageInterface)
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
	// UpdateScoreParams will update the scoring parameters of GossipSub
	UpdateScoreParams(logger *zap.Logger)

	// used for tests and api
	PeersByTopic() ([]peer.ID, map[string][]peer.ID)
}

// GetValidatorStats returns stats of validators, including the following:
//   - the amount of validators in the network
//   - the amount of active validators in the network (i.e. not slashed or existed)
//   - the amount of validators assigned to this operator
type GetValidatorStats func() (uint64, uint64, uint64, error)
