package topics

import (
	"context"
	"github.com/bloxapp/ssv/network/forks"
	"github.com/bloxapp/ssv/network/peers"
	"github.com/libp2p/go-libp2p-core/discovery"
	"github.com/libp2p/go-libp2p-core/host"
	"github.com/libp2p/go-libp2p-core/peer"
	pubsub "github.com/libp2p/go-libp2p-pubsub"
	"github.com/pkg/errors"
	"go.uber.org/zap"
	"net"
	"time"
)

const (
	// subscriptionRequestLimit sets an upper bound for the number of topic we are allowed to subscribe to
	subscriptionRequestLimit = 2128
)

// the following are kept in vars to allow flexibility (e.g. in tests)
var (
	// validationQueueSize is the size that we assign to the validation queue
	validationQueueSize = 128
	// outboundQueueSize is the size that we assign to the outbound message queue
	outboundQueueSize = 32
	// scoreInspectInterval is the interval for performing score inspect, which goes over all peers scores
	scoreInspectInterval = time.Minute
)

// PububConfig is the needed config to instantiate pubsub
type PububConfig struct {
	Logger      *zap.Logger
	Host        host.Host
	TraceLog    bool
	StaticPeers []peer.AddrInfo
	MsgHandler  PubsubMessageHandler
	// MsgValidatorFactory accepts the topic name and returns the corresponding msg validator
	// in case we need different validators for specific topics,
	// this should be the place to map a validator to topic
	MsgValidatorFactory func(string) MsgValidatorFunc
	ScoreIndex          peers.ScoreIndex
	Scoring             *ScoringConfig
	MsgIDHandler        MsgIDHandler
	Discovery           discovery.Discovery
}

// ScoringConfig is the configuration for peer scoring
type ScoringConfig struct {
	IPWhilelist        []*net.IPNet
	IPColocationWeight float64
	AppSpecificWeight  float64
	OneEpochDuration   time.Duration
}

// DefaultScoringConfig returns the default scoring config
func DefaultScoringConfig() *ScoringConfig {
	return &ScoringConfig{
		IPColocationWeight: defaultIPColocationWeight,
		AppSpecificWeight:  defaultAppSpecificWeight,
		OneEpochDuration:   defaultOneEpochDuration,
	}
}

// PubsubBundle includes the pubsub router, plus involved components
type PubsubBundle struct {
	PS         *pubsub.PubSub
	TopicsCtrl Controller
	Resolver   MsgPeersResolver
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

// initScoring initializes scoring config
func (cfg *PububConfig) initScoring() {
	if cfg.Scoring == nil {
		cfg.Scoring = DefaultScoringConfig()
	}
}

// NewPubsub creates a new pubsub router and the necessary components
func NewPubsub(ctx context.Context, cfg *PububConfig, fork forks.Fork) (*pubsub.PubSub, Controller, error) {
	if err := cfg.validate(); err != nil {
		return nil, nil, err
	}

	sf := newSubFilter(cfg.Logger, subscriptionRequestLimit)
	psOpts := []pubsub.Option{
		pubsub.WithPeerOutboundQueueSize(outboundQueueSize),
		pubsub.WithValidateQueueSize(validationQueueSize),
		//pubsub.WithFloodPublish(true),
		pubsub.WithValidateThrottle(2048),
		//pubsub.WithSubscriptionFilter(sf),
		pubsub.WithGossipSubParams(gossipSubParam()),
		//pubsub.WithPeerFilter(func(pid peer.ID, topic string) bool {
		//	cfg.Logger.Debug("pubsubTrace: filtering peer", zap.String("id", pid.String()), zap.String("topic", topic))
		//	return true
		//}),
	}

	if cfg.Discovery != nil {
		psOpts = append(psOpts, pubsub.WithDiscovery(cfg.Discovery))
	}

	if cfg.ScoreIndex != nil {
		cfg.initScoring()
		inspector := scoreInspector(cfg.Logger.With(zap.String("who", "scoreInspector")), cfg.ScoreIndex)
		psOpts = append(psOpts, pubsub.WithPeerScore(peerScoreParams(cfg), peerScoreThresholds()),
			pubsub.WithPeerScoreInspect(inspector, scoreInspectInterval))
	}

	if cfg.MsgIDHandler != nil {
		psOpts = append(psOpts, pubsub.WithMessageIdFn(cfg.MsgIDHandler.MsgID()))
	}

	//setGlobalPubSubParams()

	if len(cfg.StaticPeers) > 0 {
		psOpts = append(psOpts, pubsub.WithDirectPeers(cfg.StaticPeers))
	}

	psOpts = append(psOpts, pubsub.WithEventTracer(newTracer(cfg.Logger, cfg.TraceLog)))

	ps, err := pubsub.NewGossipSub(ctx, cfg.Host, psOpts...)
	if err != nil {
		return nil, nil, err
	}

	ctrl := NewTopicsController(ctx, cfg.Logger, cfg.MsgHandler, cfg.MsgValidatorFactory, sf, ps, fork, nil)

	return ps, ctrl, nil
}
