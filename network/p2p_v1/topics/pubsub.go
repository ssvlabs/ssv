package topics

import (
	"context"
	"github.com/bloxapp/ssv/network/forks"
	"github.com/libp2p/go-libp2p-core/host"
	"github.com/libp2p/go-libp2p-core/peer"
	pubsub "github.com/libp2p/go-libp2p-pubsub"
	"github.com/pkg/errors"
	"github.com/prysmaticlabs/prysm/async"
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

	// scoreInspectInterval is the interval for performing score inspect, which goes over all peers scores
	//scoreInspectInterval = time.Minute
)

// PububConfig is the needed config to instantiate pubsub
type PububConfig struct {
	Logger          *zap.Logger
	Host            host.Host
	TraceLog        bool
	StaticPeers     []peer.AddrInfo
	Fork            forks.Fork
	UseMsgID        bool
	UseMsgValidator bool
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

// NewPubsub creates a new pubsub router
func NewPubsub(ctx context.Context, cfg *PububConfig) (*PubsubBundle, error) {
	if err := cfg.validate(); err != nil {
		return nil, err
	}

	var msgVal MsgValidatorFunc
	if cfg.UseMsgValidator {
		msgVal = NewSSVMsgValidator(cfg.Logger, cfg.Fork, cfg.Host.ID())
	}
	ctrl := NewTopicsController(ctx, cfg.Logger, cfg.Fork, msgVal, nil, nil)

	//scoreInspector := func(map[peer.ID]*pubsub.PeerScoreSnapshot) {
	//	// TODO
	//}

	psOpts := []pubsub.Option{
		pubsub.WithSubscriptionFilter(ctrl),
		pubsub.WithPeerOutboundQueueSize(outQueueSize),
		pubsub.WithValidateQueueSize(valQueueSize),
		pubsub.WithFloodPublish(true),
		pubsub.WithGossipSubParams(gossipSubParam()),
		// TODO: check
		//pubsub.WithPeerGater(),
		//pubsub.WithPeerFilter(),
		// TODO: add scoring
		//pubsub.WithPeerScore(peerScoreParams(), peerScoreThresholds()),
		//pubsub.WithPeerScoreInspect(scoreInspector, scoreInspectInterval),
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

// TODO
//func peerScoreThresholds() *pubsub.PeerScoreThresholds {
//	return &pubsub.PeerScoreThresholds{
//		GossipThreshold:             0,
//		PublishThreshold:            0,
//		GraylistThreshold:           0,
//		AcceptPXThreshold:           0,
//		OpportunisticGraftThreshold: 0,
//	}
//}

// TODO
//func peerScoreParams() *pubsub.PeerScoreParams {
//	return &pubsub.PeerScoreParams{
//		Topics:                      nil,
//		TopicScoreCap:               0,
//		AppSpecificScore:            nil,
//		AppSpecificWeight:           0,
//		IPColocationFactorWeight:    0,
//		IPColocationFactorThreshold: 0,
//		IPColocationFactorWhitelist: nil,
//		BehaviourPenaltyWeight:      0,
//		BehaviourPenaltyThreshold:   0,
//		BehaviourPenaltyDecay:       0,
//		DecayInterval:               0,
//		DecayToZero:                 0,
//		RetainScore:                 0,
//	}
//}

// creates a custom gossipsub parameter set.
func gossipSubParam() pubsub.GossipSubParams {
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
