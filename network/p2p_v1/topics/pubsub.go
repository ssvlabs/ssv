package topics

import (
	"context"
	"github.com/libp2p/go-libp2p-core/host"
	"github.com/libp2p/go-libp2p-core/peer"
	pubsub "github.com/libp2p/go-libp2p-pubsub"
	"github.com/pkg/errors"
	"go.uber.org/zap"
	"time"
)

var (
	// overlay parameters
	gossipSubD   = 8 // topic stable mesh target count
	gossipSubDlo = 6 // topic stable mesh low watermark
	//gossipSubDhi = 12 // topic stable mesh high watermark

	// gossipMaxIHaveLength is max number fo ihave messages to send
	// lower the maximum (default is 5000) to avoid ihave floods
	maxIHaveLength = 2000

	// gossip parameters
	mcacheLen    = 4 // number of windows to retain full messages in cache for `IWANT` responses
	mcacheGossip = 3 // number of windows to gossip about
	//gossipSubSeenTTL      = 550 // number of heartbeat intervals to retain message IDs

	// heartbeat interval
	heartbeatInterval = 700 * time.Millisecond // frequency of heartbeat, milliseconds

	// valQueueSize is the size that we assign to the validation queue
	valQueueSize = 512
	// outQueueSize is the size that we assign to the outbound message queue
	outQueueSize = 256
)

// PububConfig is the needed config to instantiate pubsub
type PububConfig struct {
	Logger      *zap.Logger
	Host        host.Host
	TraceOut    string
	TraceLog    bool
	StaticPeers []peer.AddrInfo
}

func (cfg *PububConfig) validate() error {
	if cfg.Host == nil {
		return errors.New("bad args: missing host")
	}
	if cfg.Logger == nil {
		return errors.New("bad args: missing logger")
	}
	return nil
}

// NewPubsub creates a new pubsub router
func NewPubsub(ctx context.Context, cfg *PububConfig) (*pubsub.PubSub, error) {
	if err := cfg.validate(); err != nil {
		return nil, err
	}

	psOpts := []pubsub.Option{
		// TODO: add msg id
		//pubsub.WithMessageIdFn(n.msgId),
		// TODO: add subs filter
		//pubsub.WithSubscriptionFilter(s),
		pubsub.WithPeerOutboundQueueSize(outQueueSize),
		pubsub.WithValidateQueueSize(valQueueSize),
		pubsub.WithFloodPublish(true),
		pubsub.WithGossipSubParams(pubsubGossipParam()),
		// TODO: check
		//pubsub.WithPeerGater(),
		//pubsub.WithPeerFilter(),
		// TODO: add scoring
		//pubsub.WithPeerScore(),
		//pubsub.WithPeerScoreInspect(),
	}

	setGlobalPubSubParams()

	if len(cfg.StaticPeers) > 0 {
		psOpts = append(psOpts, pubsub.WithDirectPeers(cfg.StaticPeers))
	}

	psOpts = append(psOpts, pubsub.WithEventTracer(newTracer(cfg.Logger, cfg.TraceLog)))

	if len(cfg.TraceOut) > 0 {
		tracer, err := pubsub.NewPBTracer(cfg.TraceOut)
		if err != nil {
			return nil, errors.Wrap(err, "could not create pubsub tracer")
		}
		cfg.Logger.Debug("pubusb trace file was created", zap.String("path", cfg.TraceOut))
		psOpts = append(psOpts, pubsub.WithEventTracer(tracer))
	}

	return pubsub.NewGossipSub(ctx, cfg.Host, psOpts...)
}

// creates a custom gossipsub parameter set.
func pubsubGossipParam() pubsub.GossipSubParams {
	params := pubsub.DefaultGossipSubParams()
	params.Dlo = gossipSubDlo
	params.D = gossipSubD
	params.HeartbeatInterval = heartbeatInterval
	params.HistoryLength = mcacheLen
	params.HistoryGossip = mcacheGossip
	params.MaxIHaveLength = maxIHaveLength

	return params
}

// TODO: check if needed
// We have to unfortunately set this globally in order
// to configure our message id time-cache rather than instantiating
// it with a router instance.
func setGlobalPubSubParams() {
	pubsub.TimeCacheDuration = 550 * heartbeatInterval
}
