package topics

import (
	"context"
	"net"
	"time"

	pubsub "github.com/libp2p/go-libp2p-pubsub"
	"github.com/libp2p/go-libp2p/core/discovery"
	"github.com/libp2p/go-libp2p/core/host"
	libp2pnetwork "github.com/libp2p/go-libp2p/core/network"
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/pkg/errors"
	"go.uber.org/zap"

	"github.com/ssvlabs/ssv/message/validation"
	"github.com/ssvlabs/ssv/network"
	"github.com/ssvlabs/ssv/network/commons"
	"github.com/ssvlabs/ssv/network/peers"
	"github.com/ssvlabs/ssv/network/topics/params"
	"github.com/ssvlabs/ssv/networkconfig"
	"github.com/ssvlabs/ssv/registry/storage"
)

const (
	// subscriptionRequestLimit sets an upper bound for the number of topic we are allowed to subscribe to.
	// 128 subnets + 1 safety buffer
	subscriptionRequestLimit = 128 + 1
)

// the following are kept in vars to allow flexibility (e.g. in tests)
const (
	// validationQueueSize is the size that we assign to the validation queue
	validationQueueSize = 512
	// outboundQueueSize is the size that we assign to the outbound message queue
	outboundQueueSize = 512
	// validateThrottle is the amount of goroutines used for pubsub msg validation
	validateThrottle = 8192
	// scoreInspectInterval is the interval for performing score inspect, which goes over all peers scores
	defaultScoreInspectInterval = 1 * time.Minute
	// scoreInspectLogFrequency is the frequency of logging the score inspection
	scoreInspectLogFrequency = 5
	// msgIDCacheTTL specifies how long a message ID will be remembered as seen, 6.4m (as ETH 2.0)
	msgIDCacheTTL = params.HeartbeatInterval * 550
)

// PubSubConfig is the needed config to instantiate pubsub
type PubSubConfig struct {
	NetworkConfig networkconfig.Network
	Host          host.Host
	TraceLog      bool
	StaticPeers   []peer.AddrInfo
	MsgHandler    PubsubMessageHandler
	// MsgValidator accepts the topic name and returns the corresponding msg validator
	// in case we need different validators for specific topics,
	// this should be the place to map a validator to topic
	MsgValidator messageValidator
	ScoreIndex   peers.ScoreIndex
	Scoring      *ScoringConfig
	MsgIDHandler MsgIDHandler
	Discovery    discovery.Discovery

	ValidateThrottle    int
	ValidationQueueSize int
	OutboundQueueSize   int
	MsgIDCacheTTL       time.Duration

	DisableIPRateLimit     bool
	GetValidatorStats      network.GetValidatorStats
	ScoreInspector         pubsub.ExtendedPeerScoreInspectFn
	ScoreInspectorInterval time.Duration
}

// ScoringConfig is the configuration for peer scoring
type ScoringConfig struct {
	IPWhitelist        []*net.IPNet
	IPColocationWeight float64
}

// PubsubBundle includes the pubsub router, plus involved components
type PubsubBundle struct {
	PS         *pubsub.PubSub
	TopicsCtrl Controller
	Resolver   MsgPeersResolver
}

func (cfg *PubSubConfig) init() error {
	if cfg.Host == nil {
		return errors.New("bad args: missing host")
	}
	if cfg.OutboundQueueSize == 0 {
		cfg.OutboundQueueSize = outboundQueueSize
	}
	if cfg.ValidationQueueSize == 0 {
		cfg.ValidationQueueSize = validationQueueSize
	}
	if cfg.ValidateThrottle == 0 {
		cfg.ValidateThrottle = validateThrottle
	}
	if cfg.MsgIDCacheTTL == 0 {
		cfg.MsgIDCacheTTL = msgIDCacheTTL
	}
	return nil
}

// initScoring initializes scoring config
func (cfg *PubSubConfig) initScoring() {
	if cfg.Scoring == nil {
		cfg.Scoring = DefaultScoringConfig()
	}
}

type CommitteesProvider interface {
	Committees() []*storage.Committee
}

// NewPubSub creates a new pubsub router and the necessary components
func NewPubSub(
	ctx context.Context,
	logger *zap.Logger,
	cfg *PubSubConfig,
	committeesProvider CommitteesProvider,
	gossipScoreIndex peers.GossipScoreIndex,
) (*pubsub.PubSub, Controller, error) {
	if err := cfg.init(); err != nil {
		return nil, nil, err
	}

	// Set up a SubFilter with a whitelist of known topics.
	sf := newSubFilter(logger, subscriptionRequestLimit)
	for _, topic := range commons.Topics() {
		sf.(Whitelist).Register(topic)
	}

	psOpts := []pubsub.Option{
		pubsub.WithSeenMessagesTTL(cfg.MsgIDCacheTTL),
		pubsub.WithPeerOutboundQueueSize(cfg.OutboundQueueSize),
		pubsub.WithValidateQueueSize(cfg.ValidationQueueSize),
		pubsub.WithValidateThrottle(cfg.ValidateThrottle),
		pubsub.WithSubscriptionFilter(sf),
		pubsub.WithGossipSubParams(params.GossipSubParams()),
		pubsub.WithMessageSignaturePolicy(pubsub.StrictNoSign),
		pubsub.WithMaxMessageSize(validation.MaxEncodedMsgSize),
		// pubsub.WithPeerFilter(func(pid peer.ID, topic string) bool {
		//	logger.Debug("pubsubTrace: filtering peer", zap.String("id", pid.String()), zap.String("topic", topic))
		//	return true
		// }),
	}

	if cfg.Discovery != nil {
		psOpts = append(psOpts, pubsub.WithDiscovery(cfg.Discovery))
	}

	var topicScoreFactory func(string) *pubsub.TopicScoreParams

	inspector := cfg.ScoreInspector
	inspectInterval := cfg.ScoreInspectorInterval
	if cfg.ScoreIndex != nil || inspector != nil {
		cfg.initScoring()

		// Get topic score params factory
		if cfg.GetValidatorStats == nil {
			cfg.GetValidatorStats = func() (uint64, uint64, uint64, error) {
				// default in case it was not injected
				return 100, 100, 10, nil
			}
		}

		topicScoreFactory = func(t string) *pubsub.TopicScoreParams {
			return topicScoreParams(logger, cfg, committeesProvider)(t)
		}

		// Get overall score params
		peerScoreParams := params.PeerScoreParams(cfg.NetworkConfig, cfg.MsgIDCacheTTL, cfg.DisableIPRateLimit, cfg.Scoring.IPWhitelist...)

		// Define score inspector
		if inspector == nil {
			peerConnected := func(pid peer.ID) bool {
				return cfg.Host.Network().Connectedness(pid) == libp2pnetwork.Connected
			}
			inspector = scoreInspector(logger, cfg.ScoreIndex, scoreInspectLogFrequency, peerConnected, peerScoreParams, topicScoreFactory, gossipScoreIndex)
		}
		if inspectInterval == 0 {
			inspectInterval = defaultScoreInspectInterval
		}

		// Append score params to pubsub options
		psOpts = append(psOpts, pubsub.WithPeerScore(peerScoreParams, params.PeerScoreThresholds()),
			pubsub.WithPeerScoreInspect(inspector, inspectInterval))
	}

	if cfg.MsgIDHandler != nil {
		psOpts = append(psOpts, pubsub.WithMessageIdFn(cfg.MsgIDHandler.MsgID(logger)))
	}

	if len(cfg.StaticPeers) > 0 {
		psOpts = append(psOpts, pubsub.WithDirectPeers(cfg.StaticPeers))
	}

	if cfg.TraceLog {
		psOpts = append(psOpts, pubsub.WithEventTracer(newTracer(logger)))
	}

	ps, err := pubsub.NewGossipSub(ctx, cfg.Host, psOpts...)
	if err != nil {
		return nil, nil, err
	}

	ctrl := NewTopicsController(ctx, logger, cfg.MsgHandler, cfg.MsgValidator, sf, ps, topicScoreFactory)

	return ps, ctrl, nil
}
