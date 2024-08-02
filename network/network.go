package network

import (
	"context"
	"io"

	pubsub "github.com/libp2p/go-libp2p-pubsub"
	"go.uber.org/zap"

	spectypes "github.com/ssvlabs/ssv-spec/types"
	"github.com/ssvlabs/ssv/network/discovery"
	protocolp2p "github.com/ssvlabs/ssv/protocol/v2/p2p"
	"github.com/ssvlabs/ssv/protocol/v2/ssv/queue"
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
	// UpdateDomainType switches domain type at ENR when we reach fork epoch
	UpdateDomainType(logger *zap.Logger, domain spectypes.DomainType) error
	// UpdateScoreParams will update the scoring parameters of GossipSub
	UpdateScoreParams(logger *zap.Logger)
	// Returns the pubsub object
	GetPubSub() *pubsub.PubSub
	// Get discovery service
	GetDiscoveryService() discovery.Service
}

// GetValidatorStats returns stats of validators, including the following:
//   - the amount of validators in the network
//   - the amount of active validators in the network (i.e. not slashed or existed)
//   - the amount of validators assigned to this operator
type GetValidatorStats func() (uint64, uint64, uint64, error)
