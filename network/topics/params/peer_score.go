package params

import (
	"github.com/libp2p/go-libp2p-core/peer"
	pubsub "github.com/libp2p/go-libp2p-pubsub"
	"net"
	"time"
)

const (
	gossipThreshold = -4000
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
	topicScoreCap := 32.72 // TODO: topicScoreCap = maxPositiveScore / 2
	behaviourPenaltyThreshold := 6.0
	behaviourPenaltyDecay := scoreDecay(oneEpoch*10, oneEpoch)
	targetVal, _ := decayConvergence(behaviourPenaltyDecay, 8.0) // TODO: rate is hard-coded
	targetVal = targetVal - behaviourPenaltyThreshold
	behaviourPenaltyWeight := gossipThreshold / (targetVal * targetVal)

	if ipColocationWeight == 0 {
		ipColocationWeight = -topicScoreCap
	}
	if ipColocationThreshold == 0 {
		ipColocationThreshold = 3
	}
	return &pubsub.PeerScoreParams{
		Topics:        make(map[string]*pubsub.TopicScoreParams),
		TopicScoreCap: topicScoreCap,
		AppSpecificScore: func(p peer.ID) float64 {
			// TODO: expose
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
		RetainScore:                 oneEpoch * 100,
	}
}
