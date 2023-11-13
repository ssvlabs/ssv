package params

import (
	"net"
	"time"

	pubsub "github.com/libp2p/go-libp2p-pubsub"
	"github.com/libp2p/go-libp2p/core/peer"
)

const (
	// Thresholds
	gossipThreshold             = -4000
	publishThreshold            = -8000
	graylistThreshold           = -16000
	acceptPXThreshold           = 100
	opportunisticGraftThreshold = 5

	// Overall parameters
	topicScoreCap = 32.72
	decayInterval = time.Second * 12 // One slot
	decayToZero   = 0.01
	retainScore   = 38400

	// P5
	appSpecificWeight = 0

	// P6
	ipColocationFactorThreshold = 10
	ipColocationFactorWeight    = -topicScoreCap

	// P7
	// behaviourPenaltyDecay     = 0.9857119009006162
	behaviourPenaltyThreshold = 6
	// behaviourPenaltyWeight    = -15.879335171059182
)

// PeerScoreThresholds returns the thresholds to use for peer scoring
func PeerScoreThresholds() *pubsub.PeerScoreThresholds {
	return &pubsub.PeerScoreThresholds{
		GossipThreshold:             gossipThreshold,
		PublishThreshold:            publishThreshold,
		GraylistThreshold:           graylistThreshold,
		AcceptPXThreshold:           acceptPXThreshold,
		OpportunisticGraftThreshold: opportunisticGraftThreshold,
	}
}

// PeerScoreParams returns peer score params according to the given options
func PeerScoreParams(oneEpoch, msgIDCacheTTL time.Duration, ipWhilelist ...*net.IPNet) *pubsub.PeerScoreParams {
	if oneEpoch == 0 {
		oneEpoch = oneEpochDuration
	}

	// P7 calculation
	behaviourPenaltyDecay := scoreDecay(oneEpoch*10, decayInterval)
	targetVal, _ := decayConvergence(behaviourPenaltyDecay, 10.0/slotsPerEpoch)
	targetVal = targetVal - behaviourPenaltyThreshold
	behaviourPenaltyWeight := gossipThreshold / (targetVal * targetVal)

	return &pubsub.PeerScoreParams{
		Topics: make(map[string]*pubsub.TopicScoreParams),
		// Overall parameters
		TopicScoreCap: topicScoreCap,
		DecayInterval: decayInterval,
		DecayToZero:   decayToZero,
		RetainScore:   retainScore,
		SeenMsgTTL:    msgIDCacheTTL,

		// P5
		AppSpecificScore: func(p peer.ID) float64 {
			return 0
		},
		AppSpecificWeight: appSpecificWeight,

		// P6
		IPColocationFactorWeight:    ipColocationFactorWeight,
		IPColocationFactorThreshold: ipColocationFactorThreshold,
		IPColocationFactorWhitelist: ipWhilelist,

		// P7
		BehaviourPenaltyWeight:    behaviourPenaltyWeight,
		BehaviourPenaltyThreshold: behaviourPenaltyThreshold,
		BehaviourPenaltyDecay:     behaviourPenaltyDecay,
	}
}
