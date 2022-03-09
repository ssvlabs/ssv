package topics

import (
	"context"
	"github.com/bloxapp/ssv/network/forks"
	"github.com/bloxapp/ssv/network/p2p_v1/peers"
	"github.com/libp2p/go-libp2p-core/host"
	"github.com/libp2p/go-libp2p-core/peer"
	pubsub "github.com/libp2p/go-libp2p-pubsub"
	"github.com/pkg/errors"
	"github.com/prysmaticlabs/prysm/async"
	"go.uber.org/zap"
	"net"
	"time"
)

var (
	// validationQueueSize is the size that we assign to the validation queue
	validationQueueSize = 512
	// outboundQueueSize is the size that we assign to the outbound message queue
	outboundQueueSize = 256
	// scoreInspectInterval is the interval for performing score inspect, which goes over all peers scores
	scoreInspectInterval = time.Minute
)

// PububConfig is the needed config to instantiate pubsub
type PububConfig struct {
	Logger      *zap.Logger
	Host        host.Host
	TraceLog    bool
	StaticPeers []peer.AddrInfo
	Fork        forks.Fork
	UseMsgID    bool
	// MsgValidatorFactory accepts the topic name and returns the corresponding msg validator
	// in case we need different validators for specific topics,
	// this should be the place to map a validator to topic
	MsgValidatorFactory func(string) MsgValidatorFunc
	ScoreIndex          peers.ScoreIndex
	Scoring             *ScoringConfig
}

// ScoringConfig is the configuration for peer scoring
type ScoringConfig struct {
	IPWhilelist        []*net.IPNet
	IPColocationWeight float64
	AppSpecificWeight  float64
	OneEpochDuration   time.Duration
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
		cfg.Scoring = &ScoringConfig{
			IPColocationWeight: defaultIPColocationWeight,
			AppSpecificWeight:  defaultAppSpecificWeight,
			OneEpochDuration:   defaultOneEpochDuration,
		}
	}
}

// NewPubsub creates a new pubsub router
func NewPubsub(ctx context.Context, cfg *PububConfig) (*PubsubBundle, error) {
	if err := cfg.validate(); err != nil {
		return nil, err
	}

	ctrl := NewTopicsController(ctx, cfg.Logger, cfg.Fork, cfg.MsgValidatorFactory, nil, nil)

	psOpts := []pubsub.Option{
		pubsub.WithSubscriptionFilter(ctrl),
		pubsub.WithPeerOutboundQueueSize(outboundQueueSize),
		pubsub.WithValidateQueueSize(validationQueueSize),
		pubsub.WithFloodPublish(true),
		pubsub.WithGossipSubParams(gossipSubParam()),
		// TODO: check
		//pubsub.WithPeerGater(),
		//pubsub.WithPeerFilter(),
	}

	if cfg.ScoreIndex != nil {
		cfg.initScoring()
		inspector := scoreInspector(cfg.Logger.With(zap.String("who", "scoreInspector")), cfg.ScoreIndex)
		psOpts = append(psOpts, pubsub.WithPeerScore(peerScoreParams(cfg), peerScoreThresholds()),
			pubsub.WithPeerScoreInspect(inspector, scoreInspectInterval))
	}

	var midHandler MsgIDHandler
	if cfg.UseMsgID {
		midHandler = newMsgIDHandler(cfg.Logger, time.Minute*2)
		async.RunEvery(ctx, time.Minute*3, midHandler.GC)
		psOpts = append(psOpts, pubsub.WithMessageIdFn(midHandler.MsgID()))
	}

	setGlobalPubSubParams()

	if len(cfg.StaticPeers) > 0 {
		psOpts = append(psOpts, pubsub.WithDirectPeers(cfg.StaticPeers))
	}

	psOpts = append(psOpts, pubsub.WithEventTracer(newTracer(cfg.Logger, cfg.TraceLog)))

	ps, err := pubsub.NewGossipSub(ctx, cfg.Host, psOpts...)
	if err != nil {
		return nil, err
	}
	ctrl.WithPubsub(ps)

	return &PubsubBundle{
		PS:         ps,
		TopicsCtrl: ctrl,
		Resolver:   midHandler,
	}, nil
}
