package p2p

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"github.com/libp2p/go-libp2p"
	"github.com/libp2p/go-libp2p-core/peer"
	pubsub "github.com/libp2p/go-libp2p-pubsub"
	"github.com/pkg/errors"
	"go.uber.org/zap"

	"github.com/bloxapp/ssv/ibft/proto"
	"github.com/bloxapp/ssv/network"
)

const (
	// DiscoveryInterval is how often we re-publish our mDNS records.
	DiscoveryInterval = time.Second

	// DiscoveryServiceTag is used in our mDNS advertisements to discover other chat peers.
	DiscoveryServiceTag = "bloxstaking.ssv"

	// MsgChanSize is the buffer size of the message channel
	MsgChanSize = 128

	topicFmt = "bloxstaking.ssv.%s"
)

type message struct {
	Lambda []byte               `json:"lambda"`
	Msg    *proto.SignedMessage `json:"msg"`
}

type listener struct {
	lambda []byte
	ch     chan *proto.SignedMessage
}

// p2pNetwork implements network.Network interface using P2P
type p2pNetwork struct {
	ctx           context.Context
	hostID        peer.ID
	topic         *pubsub.Topic
	sub           *pubsub.Subscription
	listenersLock sync.Locker
	listeners     []listener
	logger        *zap.Logger
}

// New is the constructor of p2pNetworker
func New(ctx context.Context, logger *zap.Logger, topicName string) (network.Network, error) {
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

	n := &p2pNetwork{
		ctx:           ctx,
		hostID:        host.ID(),
		topic:         topic,
		sub:           sub,
		listenersLock: &sync.Mutex{},
		logger:        logger,
	}

	go n.listen()

	return n, nil
}

// Broadcast propagates a signed message to all peers
func (n *p2pNetwork) Broadcast(lambda []byte, msg *proto.SignedMessage) error {
	msgBytes, err := json.Marshal(message{
		Lambda: lambda,
		Msg:    msg,
	})
	if err != nil {
		return errors.Wrap(err, "failed to marshal message")
	}

	return n.topic.Publish(n.ctx, msgBytes)
}

// ReceivedMsgChan return a channel with messages
func (n *p2pNetwork) ReceivedMsgChan(_ uint64, lambda []byte) <-chan *proto.SignedMessage {
	ls := listener{
		lambda: lambda,
		ch:     make(chan *proto.SignedMessage, MsgChanSize),
	}

	n.listenersLock.Lock()
	n.listeners = append(n.listeners, ls)
	n.listenersLock.Unlock()

	return ls.ch
}

// ReceivedMsgChan return a channel with messages
func (n *p2pNetwork) listen() {
	for {
		select {
		case <-n.ctx.Done():
			if err := n.topic.Close(); err != nil {
				n.logger.Error("failed to close topic", zap.Error(err))
			}

			n.sub.Cancel()
		default:
			msg, err := n.sub.Next(n.ctx)
			if err != nil {
				n.logger.Error("failed to get message from subscription topic", zap.Error(err))
				return
			}

			var cm message
			if err := json.Unmarshal(msg.Data, &cm); err != nil {
				n.logger.Error("failed to unmarshal message", zap.Error(err))
				continue
			}

			for _, ls := range n.listeners {
				go func(ls listener) {
					if !bytes.Equal(ls.lambda, cm.Lambda) {
						return
					}

					ls.ch <- cm.Msg
				}(ls)
			}
		}
	}
}

func getTopic(topicName string) string {
	return fmt.Sprintf(topicFmt, topicName)
}
