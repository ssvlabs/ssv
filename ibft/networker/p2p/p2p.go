package p2p

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"github.com/bloxapp/ssv/ibft/networker"

	"github.com/libp2p/go-libp2p"
	pubsub "github.com/libp2p/go-libp2p-pubsub"
	"github.com/pkg/errors"
	"go.uber.org/zap"

	"github.com/bloxapp/ssv/ibft/types"
)

const (
	// DiscoveryInterval is how often we re-publish our mDNS records.
	DiscoveryInterval = time.Hour

	// DiscoveryServiceTag is used in our mDNS advertisements to discover other chat peers.
	DiscoveryServiceTag = "bloxstaking.ssv"

	topicFmt = "bloxstaking.ssv.%s"
)

// p2pNetworker implements networker.Networker interface using P2P
type p2pNetworker struct {
	ctx    context.Context
	topic  *pubsub.Topic
	logger *zap.Logger

	// TODO: Refactor that out
	pipelines map[types.RoundState]map[string][]networker.PipelineFunc
	locks     map[string]*sync.Mutex
}

// New is the constructor of p2pNetworker
func New(ctx context.Context, logger *zap.Logger, topicName string) (networker.Networker, error) {
	// Create a new libp2p Host that listens on a random TCP port
	host, err := libp2p.New(ctx, libp2p.ListenAddrStrings("/ip4/0.0.0.0/tcp/0"))
	if err != nil {
		return nil, errors.Wrap(err, "failed to create a new P2P host")
	}
	logger = logger.With(zap.String("id", host.ID().String()), zap.String("topic", topicName))
	logger.Info("created a new peer")

	// Create a new PubSub service using the GossipSub router
	ps, err := pubsub.NewGossipSub(ctx, host)
	if err != nil {
		return nil, errors.Wrap(err, "failed to create PubSub service")
	}

	// Setup local mDNS discovery
	if err := setupDiscovery(ctx, logger, host); err != nil {
		return nil, errors.Wrap(err, "failed to setup discovery")
	}

	// Join the pubsub topic
	topic, err := ps.Join(getTopic(topicName))
	if err != nil {
		return nil, errors.Wrap(err, "failed to join to topic")
	}

	// And subscribe to it
	sub, err := topic.Subscribe()
	if err != nil {
		return nil, errors.Wrap(err, "failed to subscribe on topic")
	}

	ntw := &p2pNetworker{
		ctx:    ctx,
		topic:  topic,
		logger: logger,

		pipelines: make(map[types.RoundState]map[string][]networker.PipelineFunc),
		locks:     make(map[string]*sync.Mutex),
	}

	go func() {
		for {
			msg, err := sub.Next(ctx)
			if err != nil {
				logger.Error("failed to get message from subscription topic", zap.Error(err))
				return
			}

			// Only forward messages delivered by others
			if msg.ReceivedFrom == host.ID() {
				logger.Debug("ignore own message")
				continue
			}

			cm := &types.SignedMessage{}
			if err := json.Unmarshal(msg.Data, cm); err != nil {
				logger.Error("failed to unmarshal message", zap.Error(err))
				continue
			}

			for id, pipelineForType := range ntw.pipelines[cm.Message.Type] {
				if _, ok := ntw.locks[id]; !ok {
					ntw.locks[id] = &sync.Mutex{}
				}

				ntw.locks[id].Lock()
				for _, item := range pipelineForType {
					if err := item(cm); err != nil {
						logger.Error("failed to execute pipeline for node", zap.Error(err), zap.String("node_id", id))
						ntw.locks[id].Unlock()
						break
					}
				}
				ntw.locks[id].Unlock()
			}
		}
	}()

	return ntw, nil
}

// SetMessagePipeline sets a pipeline for a message to go through before it's sent to the msg channel.
// Message validation and processing should happen in the pipeline
func (n *p2pNetworker) SetMessagePipeline(id string, roundState types.RoundState, pipeline []networker.PipelineFunc) {
	if _, ok := n.locks[id]; !ok {
		n.locks[id] = &sync.Mutex{}
	}

	if _, ok := n.pipelines[roundState]; !ok {
		n.pipelines[roundState] = make(map[string][]networker.PipelineFunc)
	}

	n.locks[id].Lock()
	n.pipelines[roundState][id] = pipeline
	n.locks[id].Unlock()
}

// Broadcast propagates a signed message to all peers
func (n *p2pNetworker) Broadcast(msg *types.SignedMessage) error {
	msgBytes, err := json.Marshal(msg)
	if err != nil {
		return errors.Wrap(err, "failed to marshal message")
	}

	return n.topic.Publish(n.ctx, msgBytes)
}

func getTopic(topicName string) string {
	return fmt.Sprintf(topicFmt, topicName)
}
