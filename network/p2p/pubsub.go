package p2p

import (
	"github.com/libp2p/go-libp2p-core/peer"
	pubsub "github.com/libp2p/go-libp2p-pubsub"
	"github.com/pkg/errors"
	"go.uber.org/zap"
	"time"
)

const (
	// overlay parameters
	gossipSubD   = 8 // topic stable mesh target count
	gossipSubDlo = 6 // topic stable mesh low watermark
	//gossipSubDhi = 12 // topic stable mesh high watermark

	// gossip parameters
	//gossipSubMcacheLen    = 6   // number of windows to retain full messages in cache for `IWANT` responses
	//gossipSubMcacheGossip = 3   // number of windows to gossip about
	//gossipSubSeenTTL      = 550 // number of heartbeat intervals to retain message IDs

	// heartbeat interval
	gossipSubHeartbeatInterval = 700 * time.Millisecond // frequency of heartbeat, milliseconds

	// pubsubQueueSize is the size that we assign to our validation queue and outbound message queue for
	// gossipsub.
	pubsubQueueSize = 600
)

func (n *p2pNetwork) newGossipPubsub(cfg *Config) (*pubsub.PubSub, error) {
	// Gossipsub registration is done before we add in any new peers
	// due to libp2p's gossipsub implementation not taking into
	// account previously added peers when creating the gossipsub
	// object.
	psOpts := []pubsub.Option{
		//pubsub.WithMessageSignaturePolicy(pubsub.StrictNoSign),
		//pubsub.WithNoAuthor(),
		//pubsub.WithMessageIdFn(n.msgId),
		//pubsub.WithSubscriptionFilter(s),
		pubsub.WithPeerOutboundQueueSize(pubsubQueueSize),
		pubsub.WithValidateQueueSize(pubsubQueueSize),
		pubsub.WithFloodPublish(true),
		pubsub.WithGossipSubParams(pubsubGossipParam()),
	}
	if len(cfg.ExporterPeerID) > 0 {
		exporterPeerID, err := peerFromString(cfg.ExporterPeerID)
		if err != nil {
			n.logger.Error("could not parse exporter peer id", zap.Error(err))
		} else {
			psOpts = append(psOpts, pubsub.WithDirectPeers([]peer.AddrInfo{{ID: exporterPeerID}}))
		}
	}

	if len(cfg.PubSubTraceOut) > 0 {
		tracer, err := pubsub.NewPBTracer(cfg.PubSubTraceOut)
		if err != nil {
			return nil, errors.Wrap(err, "could not create pubsub tracer")
		}
		n.logger.Debug("pubusb trace file was created", zap.String("path", cfg.PubSubTraceOut))
		psOpts = append(psOpts, pubsub.WithEventTracer(tracer))
	}

	setGlobalPubSubParameters()

	// Create a new PubSub service using the GossipSub router
	return pubsub.NewGossipSub(n.ctx, n.host, psOpts...)
}

// creates a custom gossipsub parameter set.
func pubsubGossipParam() pubsub.GossipSubParams {
	gParams := pubsub.DefaultGossipSubParams()
	gParams.Dlo = gossipSubDlo
	gParams.D = gossipSubD
	gParams.HeartbeatInterval = gossipSubHeartbeatInterval
	//gParams.HistoryLength = gossipSubMcacheLen
	//gParams.HistoryGossip = gossipSubMcacheGossip

	// Set a larger gossip history to ensure that slower
	// messages have a longer time to be propagated. This
	// comes with the tradeoff of larger memory usage and
	// size of the seen message cache.
	//if features.Get().EnableLargerGossipHistory {
	gParams.HistoryLength = 12
	gParams.HistoryGossip = 5
	//}
	return gParams
}

// We have to unfortunately set this globally in order
// to configure our message id time-cache rather than instantiating
// it with a router instance.
func setGlobalPubSubParameters() {
	pubsub.TimeCacheDuration = 550 * gossipSubHeartbeatInterval
}

//func (n *p2pNetwork) msgId(pmsg *pubsub_pb.Message) string {
//	if pmsg == nil || pmsg.Data == nil || pmsg.Topic == nil {
//		msg := make([]byte, 20)
//		copy(msg, "invalid")
//		return string(msg)
//	}
//	// TODO: calculate a specific message id, currently hash(validator PK (topic) + msg data)
//	data := bytes.Join([][]byte{[]byte(*pmsg.Topic), pmsg.Data[:]}, []byte{})
//	h := sha256.Sum256(data[:])
//	val := hex.EncodeToString(h[:20])
//	n.logger.Debug("msg id", zap.String("val", val))
//	return val
//}
