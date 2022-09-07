package params

import (
	"github.com/libp2p/go-libp2p-core/peer"
	pubsub "github.com/libp2p/go-libp2p-pubsub"
	"net"
	"time"
)

const (
	gossipThreshold              = -4000
	defaultIPColocationThreshold = 10 // TODO: check a lower value such as in ETH (3)
)

// PeerScoreThresholds returns the thresholds to use for peer scoring
func PeerScoreThresholds() *pubsub.PeerScoreThresholds {
	return &pubsub.PeerScoreThresholds{
		GossipThreshold:             gossipThreshold,
		PublishThreshold:            -8000,
		GraylistThreshold:           -16000,
		AcceptPXThreshold:           100,
		OpportunisticGraftThreshold: 5,
	}
}

// PeerScoreParams returns peer score params according to the given options
func PeerScoreParams(oneEpoch, msgIDCacheTTL time.Duration, ipColocationWeight float64, ipColocationThreshold int, ipWhilelist ...*net.IPNet) *pubsub.PeerScoreParams {
	if oneEpoch == 0 {
		oneEpoch = oneEpochDuration
	}
	// TODO: topicScoreCap = maxPositiveScore / 2
	topicScoreCap := 32.72
	behaviourPenaltyThreshold := 16.0 // using a larger threshold than ETH (6) to reduce the effect of behavioural penalty
	behaviourPenaltyDecay := scoreDecay(oneEpoch*10, oneEpoch)
	// TODO: rate (10.0) should be injected to this function
	targetVal, _ := decayConvergence(behaviourPenaltyDecay, 10.0/slotsPerEpoch)
	targetVal = targetVal - behaviourPenaltyThreshold
	behaviourPenaltyWeight := gossipThreshold / (targetVal * targetVal)

	if ipColocationWeight == 0 {
		ipColocationWeight = -topicScoreCap
	}
	if ipColocationThreshold == 0 {
		ipColocationThreshold = defaultIPColocationThreshold
	}
	return &pubsub.PeerScoreParams{
		Topics:        make(map[string]*pubsub.TopicScoreParams),
		TopicScoreCap: topicScoreCap,
		AppSpecificScore: func(p peer.ID) float64 {
			// TODO: implement
			return 0
		},
		AppSpecificWeight:           1,
		IPColocationFactorWeight:    ipColocationWeight,
		IPColocationFactorThreshold: ipColocationThreshold,
		IPColocationFactorWhitelist: ipWhilelist,
		SeenMsgTTL:                  msgIDCacheTTL,
		BehaviourPenaltyWeight:      behaviourPenaltyWeight,
		BehaviourPenaltyThreshold:   behaviourPenaltyThreshold,
		BehaviourPenaltyDecay:       behaviourPenaltyDecay,
		DecayInterval:               oneEpoch,
		DecayToZero:                 decayToZero,
		// RetainScore is the time to remember counters for a disconnected peer
		// TODO: ETH uses 100 epoch, we reduced it to 10 until scoring will be more mature
		RetainScore: oneEpoch * 10,
	}
}
