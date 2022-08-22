package topics

import (
	"github.com/bloxapp/ssv/network/forks"
	"github.com/bloxapp/ssv/network/peers"
	"github.com/libp2p/go-libp2p-core/peer"
	pubsub "github.com/libp2p/go-libp2p-pubsub"
	"github.com/pkg/errors"
	"go.uber.org/zap"
	"math"
	"time"
)

const (
	defaultIPColocationWeight = -32.0
	// defaultOneEpochDuration is slots-per-epoch * slot-duration
	defaultOneEpochDuration = (12 * time.Second) * 32

	// subnetsTotalWeight specifies the total scoring weight that we apply to subnets all-together,
	subnetsTotalWeight = 64
	// decidedTopicWeight specifies the scoring weight that we apply to decided topic
	decidedTopicWeight = 0.7
	// maxInMeshScore describes the max score a peer can attain from being in the mesh
	maxInMeshScore = 30
	// maxFirstDeliveryScore describes the max score a peer can obtain from first deliveries
	maxFirstDeliveryScore = 90
	// decayToZero specifies the terminal value that we will use when decaying
	// a value.
	decayToZero = 0.01
	// dampeningFactor reduces the amount by which the various thresholds and caps are created.
	dampeningFactor = 90
)

// DefaultScoringConfig returns the default scoring config
func DefaultScoringConfig() *ScoringConfig {
	return &ScoringConfig{
		IPColocationWeight: defaultIPColocationWeight,
		OneEpochDuration:   defaultOneEpochDuration,
	}
}

// scoreInspector inspects scores and updates the score index accordingly
func scoreInspector(logger *zap.Logger, scoreIdx peers.ScoreIndex) pubsub.ExtendedPeerScoreInspectFn {
	return func(scores map[peer.ID]*pubsub.PeerScoreSnapshot) {
		for pid, peerScores := range scores {
			//scores := []*peers.NodeScore{
			//	{
			//		Name:  "PS_Score",
			//		Value: peerScores.Score,
			//	}, {
			//		Name:  "PS_BehaviourPenalty",
			//		Value: peerScores.BehaviourPenalty,
			//	}, {
			//		Name:  "PS_IPColocationFactor",
			//		Value: peerScores.IPColocationFactor,
			//	},
			//}
			logger.Debug("peer scores", zap.String("peer", pid.String()),
				zap.Any("peerScores", peerScores))
			metricsPubsubPeerScoreInspect.WithLabelValues(pid.String()).Set(peerScores.Score)
			//err := scoreIdx.Score(pid, scores...)
			//if err != nil {
			//	logger.Warn("could not score peer", zap.String("peer", pid.String()), zap.Error(err))
			//} else {
			//	logger.Debug("peer scores were updated", zap.String("peer", pid.String()),
			//		zap.Any("scores", scores), zap.Any("topicScores", peerScores.Topics))
			//}
		}
	}
}

// peerScoreThresholds returns the thresholds to use for peer scoring
func peerScoreThresholds() *pubsub.PeerScoreThresholds {
	return &pubsub.PeerScoreThresholds{
		GossipThreshold:             -4000,
		PublishThreshold:            -8000,
		GraylistThreshold:           -16000,
		AcceptPXThreshold:           100,
		OpportunisticGraftThreshold: 5,
	}
}

// determines the decay rate from the provided time period till
// the decayToZero value. Ex: ( 1 -> 0.01)
func scoreDecay(totalDurationDecay time.Duration, oneEpochDuration time.Duration) float64 {
	numOfTimes := totalDurationDecay / oneEpochDuration
	return math.Pow(decayToZero, 1/float64(numOfTimes))
}

// peerScoreParams returns peer score params in the router level
func peerScoreParams(cfg *PububConfig) *pubsub.PeerScoreParams {
	return &pubsub.PeerScoreParams{
		Topics:        make(map[string]*pubsub.TopicScoreParams),
		TopicScoreCap: 53.749999999999986,
		AppSpecificScore: func(p peer.ID) float64 {
			return 0
		},
		//AppSpecificScore: appSpecificScore(
		//	cfg.Logger.With(zap.String("who", "appSpecificScore")),
		//	cfg.ScoreIndex),
		AppSpecificWeight:           0,
		IPColocationFactorWeight:    cfg.Scoring.IPColocationWeight,
		IPColocationFactorThreshold: 10, // max 10 peers from the same IP
		IPColocationFactorWhitelist: cfg.Scoring.IPWhilelist,
		BehaviourPenaltyWeight:      -15.92,
		BehaviourPenaltyThreshold:   6,
		SeenMsgTTL:                  cfg.MsgIDCacheTTL,
		BehaviourPenaltyDecay:       scoreDecay(cfg.Scoring.OneEpochDuration*10, cfg.Scoring.OneEpochDuration),
		DecayInterval:               cfg.Scoring.OneEpochDuration,
		DecayToZero:                 decayToZero,
		RetainScore:                 cfg.Scoring.OneEpochDuration * 10,
	}
}

//
//func appSpecificScore(logger *zap.Logger, scoreIdx peers.ScoreIndex) func(p peer.ID) float64 {
//	return func(p peer.ID) float64 {
//		// TODO: complete
//		scores, err := scoreIdx.GetScore(p, "")
//		if err != nil {
//			logger.Warn("could not get score for peer", zap.String("peer", p.String()), zap.Error(err))
//			return 0.0
//		}
//		var res float64
//		for _, s := range scores {
//			res += s.Value
//		}
//		return res
//	}
//}

// topicScoreParams factory for creating scoring params for topics
func topicScoreParams(cfg *PububConfig, f forks.Fork) func(string) *pubsub.TopicScoreParams {
	decidedTopic := f.GetTopicFullName(f.DecidedTopic())
	return func(s string) *pubsub.TopicScoreParams {
		switch s {
		case decidedTopic:
			return decidedTopicScoreParams(cfg, f)
		default:
			return subnetTopicScoreParams(cfg, f)
		}
	}
}

// decidedTopicScoreParams returns the scoring params for the decided topic,
// based on lighthouse parameters for block-topic, with some changes from prysm and alignment to ssv:
// https://gist.github.com/blacktemplar/5c1862cb3f0e32a1a7fb0b25e79e6e2c
func decidedTopicScoreParams(cfg *PububConfig, f forks.Fork) *pubsub.TopicScoreParams {
	inMeshTime := cfg.Scoring.OneEpochDuration
	decayEpoch := time.Duration(5)
	blocksPerEpoch := uint64(32) // TODO: check as we don't know this number in ssv
	meshWeight := -0.717         // TODO: check why the value is different than
	//if !meshDeliveryIsScored {
	//	// Set the mesh weight as zero as a temporary measure, so as to prevent
	//	// the average nodes from being penalised.
	//	meshWeight = 0
	//}
	return &pubsub.TopicScoreParams{
		TopicWeight:                     decidedTopicWeight,
		TimeInMeshWeight:                maxInMeshScore / inMeshCap(inMeshTime),
		TimeInMeshQuantum:               inMeshTime,
		TimeInMeshCap:                   inMeshCap(inMeshTime),
		FirstMessageDeliveriesWeight:    1,
		FirstMessageDeliveriesDecay:     scoreDecay(cfg.Scoring.OneEpochDuration*20, cfg.Scoring.OneEpochDuration),
		FirstMessageDeliveriesCap:       23,
		MeshMessageDeliveriesWeight:     meshWeight,
		MeshMessageDeliveriesDecay:      scoreDecay(decayEpoch*cfg.Scoring.OneEpochDuration, cfg.Scoring.OneEpochDuration),
		MeshMessageDeliveriesCap:        float64(blocksPerEpoch * uint64(decayEpoch)),
		MeshMessageDeliveriesThreshold:  float64(blocksPerEpoch*uint64(decayEpoch)) / 10,
		MeshMessageDeliveriesWindow:     2 * time.Second,
		MeshMessageDeliveriesActivation: 4 * cfg.Scoring.OneEpochDuration,
		MeshFailurePenaltyWeight:        meshWeight,
		MeshFailurePenaltyDecay:         scoreDecay(decayEpoch*cfg.Scoring.OneEpochDuration, cfg.Scoring.OneEpochDuration),
		InvalidMessageDeliveriesWeight:  0.0, // TODO: enable once validation is in place
		InvalidMessageDeliveriesDecay:   0.1,
		//InvalidMessageDeliveriesWeight:  -140.4475,
		//InvalidMessageDeliveriesDecay:   scoreDecay(invalidDecayPeriod),
	}
}

// subnetTopicScoreParams returns the scoring params for a subnet topic
// based on lighthouse parameters for attestation subnet, with some changes from prysm and alignment to ssv:
// https://gist.github.com/blacktemplar/5c1862cb3f0e32a1a7fb0b25e79e6e2c
func subnetTopicScoreParams(cfg *PububConfig, f forks.Fork) *pubsub.TopicScoreParams {
	subnetCount := uint64(f.Subnets())
	// Get weight for each specific subnet.
	topicWeight := subnetsTotalWeight / float64(subnetCount)
	// TODO: get active subnets/validators
	activeValidators := uint64(128)
	subnetWeight := activeValidators / subnetCount
	// Determine the amount of validators expected in a subnet in a single slot.
	numPerSlot := time.Duration(subnetWeight / uint64(32))
	if numPerSlot == 0 {
		return nil
	}
	//comsPerSlot := committeeCountPerSlot(activeValidators)
	//exceedsThreshold := comsPerSlot >= 2*subnetCount/uint64(32)
	firstDecay := time.Duration(1)
	meshDecay := time.Duration(4)
	//if exceedsThreshold {
	//	firstDecay = 4
	//	meshDecay = 16
	//}
	rate := numPerSlot * 2 / time.Duration(gsD)
	if rate == 0 {
		return nil
	}
	// Determine expected first deliveries based on the message rate.
	firstMessageCap, err := decayLimit(scoreDecay(firstDecay*cfg.Scoring.OneEpochDuration, cfg.Scoring.OneEpochDuration), float64(rate))
	if err != nil {
		return nil
	}
	firstMessageWeight := maxFirstDeliveryScore / firstMessageCap
	// Determine expected mesh deliveries based on message rate applied with a dampening factor.
	meshThreshold, err := decayThreshold(scoreDecay(firstDecay*cfg.Scoring.OneEpochDuration, cfg.Scoring.OneEpochDuration),
		float64(numPerSlot)/dampeningFactor)
	if err != nil {
		return nil
	}
	meshWeight := -scoreByWeight(topicWeight, meshThreshold)
	meshCap := 4 * meshThreshold
	//invalidDecayPeriod := 50 * cfg.Scoring.OneEpochDuration
	return &pubsub.TopicScoreParams{
		TopicWeight:                     topicWeight,
		TimeInMeshWeight:                maxInMeshScore / inMeshCap(cfg.Scoring.OneEpochDuration),
		TimeInMeshQuantum:               cfg.Scoring.OneEpochDuration,
		TimeInMeshCap:                   inMeshCap(cfg.Scoring.OneEpochDuration),
		FirstMessageDeliveriesWeight:    firstMessageWeight,
		FirstMessageDeliveriesDecay:     scoreDecay(firstDecay*cfg.Scoring.OneEpochDuration, cfg.Scoring.OneEpochDuration),
		FirstMessageDeliveriesCap:       firstMessageCap,
		MeshMessageDeliveriesWeight:     -37.22,
		MeshMessageDeliveriesDecay:      scoreDecay(meshDecay*cfg.Scoring.OneEpochDuration, cfg.Scoring.OneEpochDuration),
		MeshMessageDeliveriesCap:        meshCap,
		MeshMessageDeliveriesThreshold:  meshThreshold,
		MeshMessageDeliveriesWindow:     2 * time.Second,
		MeshMessageDeliveriesActivation: 200 * time.Second,
		MeshFailurePenaltyWeight:        meshWeight,
		MeshFailurePenaltyDecay:         scoreDecay(meshDecay*cfg.Scoring.OneEpochDuration, cfg.Scoring.OneEpochDuration),
		InvalidMessageDeliveriesWeight:  0.0, // TODO: enable once validation is in place
		InvalidMessageDeliveriesDecay:   0.1,
		//InvalidMessageDeliveriesWeight:  -maxScore() / topicWeight,
		//InvalidMessageDeliveriesDecay:   scoreDecay(invalidDecayPeriod, cfg.Scoring.OneEpochDuration),
	}
}

// the cap for `inMesh` time scoring.
func inMeshCap(inMeshTime time.Duration) float64 {
	return float64((3600 * time.Second) / inMeshTime)
}

// is used to determine the threshold from the decay limit with
// a provided growth rate. This applies the decay rate to a
// computed limit.
func decayThreshold(decayRate, rate float64) (float64, error) {
	d, err := decayLimit(decayRate, rate)
	if err != nil {
		return 0, err
	}
	return d * decayRate, nil
}

// decayLimit provides the value till which a decay process will
// limit till provided with an expected growth rate.
func decayLimit(decayRate, rate float64) (float64, error) {
	if 1 <= decayRate {
		return 0, errors.Errorf("got an invalid decayLimit rate: %f", decayRate)
	}
	return rate / (1 - decayRate), nil
}

// provides the relevant score by the provided weight and threshold.
func scoreByWeight(weight, threshold float64) float64 {
	return maxScore() / (weight * threshold * threshold)
}

// maxScore attainable by a peer.
func maxScore() float64 {
	totalWeight := decidedTopicWeight + subnetsTotalWeight
	return (maxInMeshScore + maxFirstDeliveryScore) * totalWeight
}
