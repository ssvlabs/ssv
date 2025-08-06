package params

import (
	"net"
	"time"

	pubsub "github.com/libp2p/go-libp2p-pubsub"
	"github.com/libp2p/go-libp2p/core/peer"

	"github.com/ssvlabs/ssv/networkconfig"
)

const (
	// Thresholds
	gossipThreshold             = -4000
	publishThreshold            = -8000
	graylistThreshold           = -16000
	acceptPXThreshold           = 100
	opportunisticGraftThreshold = 5

	// Overall parameters
	topicScoreCap              = 32.72
	decayToZero                = 0.01
	retainScoreEpochMultiplier = 100

	// P5
	appSpecificWeight = 0

	// P6
	ipColocationFactorThreshold = 10
	ipColocationFactorWeight    = -topicScoreCap

	// P7
	behaviorPenaltyThreshold = 6
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
func PeerScoreParams(netCfg *networkconfig.Network, msgIDCacheTTL time.Duration, disableColocation bool, ipWhitelist ...*net.IPNet) *pubsub.PeerScoreParams {
	// P7 calculation
	behaviorPenaltyDecay := scoreDecay(netCfg.EpochDuration()*10, netCfg.EpochDuration())
	maxAllowedRatePerDecayInterval := 10.0
	targetVal, _ := decayConvergence(behaviorPenaltyDecay, maxAllowedRatePerDecayInterval)
	targetVal = targetVal - behaviorPenaltyThreshold
	behaviorPenaltyWeight := gossipThreshold / (targetVal * targetVal)

	finalIPColocationFactorWeight := ipColocationFactorWeight
	if disableColocation {
		finalIPColocationFactorWeight = 0
	}

	return &pubsub.PeerScoreParams{
		Topics: make(map[string]*pubsub.TopicScoreParams),
		// Overall parameters
		TopicScoreCap: topicScoreCap,
		DecayInterval: netCfg.EpochDuration(),
		DecayToZero:   decayToZero,
		RetainScore:   retainScoreEpochMultiplier * netCfg.EpochDuration(),
		SeenMsgTTL:    msgIDCacheTTL,

		// P5
		AppSpecificScore: func(p peer.ID) float64 {
			return 0
		},
		AppSpecificWeight: appSpecificWeight,

		// P6
		IPColocationFactorWeight:    finalIPColocationFactorWeight,
		IPColocationFactorThreshold: ipColocationFactorThreshold,
		IPColocationFactorWhitelist: ipWhitelist,

		// P7
		BehaviourPenaltyWeight:    behaviorPenaltyWeight,
		BehaviourPenaltyThreshold: behaviorPenaltyThreshold,
		BehaviourPenaltyDecay:     behaviorPenaltyDecay,
	}
}
