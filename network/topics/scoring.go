package topics

import (
	"github.com/bloxapp/ssv/network/forks"
	"github.com/bloxapp/ssv/network/peers"
	"github.com/bloxapp/ssv/network/topics/params"
	"github.com/libp2p/go-libp2p-core/peer"
	pubsub "github.com/libp2p/go-libp2p-pubsub"
	"go.uber.org/zap"
	"time"
)

// DefaultScoringConfig returns the default scoring config
func DefaultScoringConfig() *ScoringConfig {
	return &ScoringConfig{
		IPColocationWeight: -35.11,
		OneEpochDuration:   (12 * time.Second) * 32,
	}
}

// scoreInspector inspects scores and updates the score index accordingly
// TODO: finalize once validation is in place
func scoreInspector(logger *zap.Logger, scoreIdx peers.ScoreIndex) pubsub.ExtendedPeerScoreInspectFn {
	return func(scores map[peer.ID]*pubsub.PeerScoreSnapshot) {
		sum := 0.0
		positiveCount := 0
		negativeCount := 0
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
			if peerScores.Score < float64(0) {
				negativeCount++
			} else if peerScores.Score > float64(0) {
				positiveCount++
			}
			sum += peerScores.Score
			//err := scoreIdx.Score(pid, scores...)
			//if err != nil {
			//	logger.Warn("could not score peer", zap.String("peer", pid.String()), zap.Error(err))
			//} else {
			//	logger.Debug("peer scores were updated", zap.String("peer", pid.String()),
			//		zap.Any("scores", scores), zap.Any("topicScores", peerScores.Topics))
			//}
		}
		if len(scores) > 0 {
			metricPubsubPeerScoreAverage.Set(sum / float64(len(scores)))
		} else {
			metricPubsubPeerScoreAverage.Set(0.0)
		}
		metricPubsubPeerScorePositive.Set(float64(positiveCount))
		metricPubsubPeerScoreNegative.Set(float64(negativeCount))
	}
}

// topicScoreParams factory for creating scoring params for topics
func topicScoreParams(cfg *PububConfig, f forks.Fork) func(string) *pubsub.TopicScoreParams {
	decidedTopic := f.GetTopicFullName(f.DecidedTopic())
	return func(t string) *pubsub.TopicScoreParams {
		activeValidators, _, err := cfg.GetValidatorStats()
		if err != nil {
			cfg.Logger.Debug("could not read stats: active validators")
			return nil
		}
		var opts params.Options
		switch t {
		case decidedTopic:
			opts = params.NewDecidedTopicOpts(int(activeValidators), f.Subnets())
		default:
			opts = params.NewSubnetTopicOpts(int(activeValidators), f.Subnets())
		}
		tp, err := params.TopicParams(opts)
		if err != nil {
			cfg.Logger.Debug("ignoring topic score params", zap.String("topic", t), zap.Error(err))
			return nil
		}
		return tp
	}
}

//
//// decidedTopicScoreParams returns the scoring params for the decided topic,
//// based on lighthouse parameters for block-topic, with some changes from prysm and alignment to ssv:
//// https://gist.github.com/blacktemplar/5c1862cb3f0e32a1a7fb0b25e79e6e2c
//func decidedTopicScoreParams(cfg *PububConfig, f forks.Fork) *pubsub.TopicScoreParams {
//	inMeshTime := cfg.Scoring.OneEpochDuration
//	decayEpoch := time.Duration(5)
//	activeValidators, _, err := cfg.GetValidatorStats()
//	if err != nil || activeValidators < 200 {
//		// using minimum of 200 validators
//		// TODO: if there less active validators we should return nil
//		activeValidators = 200
//	}
//	blocksPerEpoch := activeValidators / 2 // assuming only half of the validators are sending messages
//	meshWeight := -0.717
//	//if !meshDeliveryIsScored {
//	//	// Set the mesh weight as zero as a temporary measure, so as to prevent
//	//	// the average nodes from being penalised.
//	//	meshWeight = 0
//	//}
//	return &pubsub.TopicScoreParams{
//		TopicWeight:                     decidedTopicWeight,
//		TimeInMeshWeight:                maxInMeshScore / inMeshCap(inMeshTime),
//		TimeInMeshQuantum:               inMeshTime,
//		TimeInMeshCap:                   inMeshCap(inMeshTime),
//		FirstMessageDeliveriesWeight:    1,
//		FirstMessageDeliveriesDecay:     scoreDecay(cfg.Scoring.OneEpochDuration*20, cfg.Scoring.OneEpochDuration),
//		FirstMessageDeliveriesCap:       23,
//		MeshMessageDeliveriesWeight:     meshWeight,
//		MeshMessageDeliveriesDecay:      scoreDecay(decayEpoch*cfg.Scoring.OneEpochDuration, cfg.Scoring.OneEpochDuration),
//		MeshMessageDeliveriesCap:        float64(blocksPerEpoch*uint64(decayEpoch)) / 2.0,
//		MeshMessageDeliveriesThreshold:  float64(blocksPerEpoch*uint64(decayEpoch)) / 100.0,
//		MeshMessageDeliveriesWindow:     2 * time.Second,
//		MeshMessageDeliveriesActivation: 4 * cfg.Scoring.OneEpochDuration,
//		MeshFailurePenaltyWeight:        meshWeight,
//		MeshFailurePenaltyDecay:         scoreDecay(decayEpoch*cfg.Scoring.OneEpochDuration, cfg.Scoring.OneEpochDuration),
//		InvalidMessageDeliveriesWeight:  0.0, // TODO: enable once validation is in place
//		InvalidMessageDeliveriesDecay:   0.1,
//		//InvalidMessageDeliveriesWeight:  -140.4475,
//		//InvalidMessageDeliveriesDecay:   scoreDecay(invalidDecayPeriod),
//	}
//}
//
//// subnetTopicScoreParams returns the scoring params for a subnet topic
//// based on lighthouse parameters for attestation subnet, with some changes from prysm and alignment to ssv:
//// https://gist.github.com/blacktemplar/5c1862cb3f0e32a1a7fb0b25e79e6e2c
//func subnetTopicScoreParams(cfg *PububConfig, f forks.Fork) (*pubsub.TopicScoreParams, error) {
//	activeValidators, _, err := cfg.GetValidatorStats()
//	if err != nil || activeValidators < 200 {
//		// using minimum of 200 validators
//		// TODO: if there less active validators we should return nil
//		activeValidators = 200
//	}
//	subnetCount := uint64(f.Subnets())
//	// Get weight for each specific subnet.
//	topicWeight := subnetsTotalWeight / float64(subnetCount)
//	subnetWeight := activeValidators / subnetCount
//	// Determine the amount of validators expected in a subnet in a single slot.
//	numPerSlot := time.Duration(subnetWeight / uint64(16))
//	if numPerSlot == 0 {
//		// using minimum of 2
//		// TODO: if zero we should return nil
//		//return nil, errors.New("got invalid num per slot: 0")
//		numPerSlot = 2
//	}
//	//comsPerSlot := committeeCountPerSlot(activeValidators)
//	//exceedsThreshold := comsPerSlot >= 2*subnetCount/uint64(32)
//	firstDecay := time.Duration(1)
//	meshDecay := time.Duration(4)
//	//if exceedsThreshold {
//	//	firstDecay = 4
//	//	meshDecay = 16
//	//}
//	rate := numPerSlot * 2 / time.Duration(gsD)
//	if rate == 0 {
//		// using minimum of 1
//		// TODO: if zero we should return nil
//		//return nil, errors.New("got invalid rate: 0")
//		rate = 1
//	}
//	// Determine expected first deliveries based on the message rate.
//	firstMessageCap, err := decayLimit(scoreDecay(firstDecay*cfg.Scoring.OneEpochDuration, cfg.Scoring.OneEpochDuration), float64(rate))
//	if err != nil {
//		return nil, err
//	}
//	firstMessageWeight := maxFirstDeliveryScore / firstMessageCap
//	// Determine expected mesh deliveries based on message rate applied with a dampening factor.
//	meshThreshold, err := decayThreshold(scoreDecay(firstDecay*cfg.Scoring.OneEpochDuration, cfg.Scoring.OneEpochDuration),
//		float64(numPerSlot)/dampeningFactor)
//	if err != nil {
//		return nil, err
//	}
//	// TODO: uncomment
//	//meshWeight := -scoreByWeight(topicWeight, meshThreshold)
//	meshWeight := -37.2
//	meshCap := 10.0 * meshThreshold
//	//invalidDecayPeriod := 50 * cfg.Scoring.OneEpochDuration
//	return &pubsub.TopicScoreParams{
//		TopicWeight:                     topicWeight,
//		TimeInMeshWeight:                maxInMeshScore / inMeshCap(cfg.Scoring.OneEpochDuration),
//		TimeInMeshQuantum:               cfg.Scoring.OneEpochDuration,
//		TimeInMeshCap:                   inMeshCap(cfg.Scoring.OneEpochDuration),
//		FirstMessageDeliveriesWeight:    firstMessageWeight,
//		FirstMessageDeliveriesDecay:     scoreDecay(firstDecay*cfg.Scoring.OneEpochDuration, cfg.Scoring.OneEpochDuration),
//		FirstMessageDeliveriesCap:       firstMessageCap,
//		MeshMessageDeliveriesWeight:     meshWeight,
//		MeshMessageDeliveriesDecay:      scoreDecay(meshDecay*cfg.Scoring.OneEpochDuration, cfg.Scoring.OneEpochDuration),
//		MeshMessageDeliveriesCap:        meshCap,
//		MeshMessageDeliveriesThreshold:  meshThreshold,
//		MeshMessageDeliveriesWindow:     2 * time.Second,
//		MeshMessageDeliveriesActivation: 1 * cfg.Scoring.OneEpochDuration,
//		MeshFailurePenaltyWeight:        meshWeight,
//		MeshFailurePenaltyDecay:         scoreDecay(meshDecay*cfg.Scoring.OneEpochDuration, cfg.Scoring.OneEpochDuration),
//		InvalidMessageDeliveriesWeight:  0.0, // TODO: enable once validation is in place
//		InvalidMessageDeliveriesDecay:   0.1,
//		//InvalidMessageDeliveriesWeight:  -maxScore() / topicWeight,
//		//InvalidMessageDeliveriesDecay:   scoreDecay(invalidDecayPeriod, cfg.Scoring.OneEpochDuration),
//	}, nil
//}
//
//// the cap for `inMesh` time scoring.
//func inMeshCap(inMeshTime time.Duration) float64 {
//	return float64((3600 * time.Second) / inMeshTime)
//}
//
//// decayThreshold is used to determine the threshold from the decay limit with
//// a provided growth rate. This applies the decay rate to a
//// computed limit.
//func decayThreshold(decayRate, rate float64) (float64, error) {
//	d, err := decayLimit(decayRate, rate)
//	if err != nil {
//		return 0, err
//	}
//	return d * decayRate, nil
//}
//
//// decayLimit provides the value till which a decay process will
//// limit till provided with an expected growth rate.
//func decayLimit(decayRate, rate float64) (float64, error) {
//	if 1 <= decayRate {
//		return 0, errors.Errorf("got an invalid decayLimit rate: %f", decayRate)
//	}
//	return rate / (1 - decayRate), nil
//}

//// provides the relevant score by the provided weight and threshold.
//func scoreByWeight(weight, threshold float64) float64 {
//	return maxScore() / (weight * threshold * threshold)
//}

//// maxScore attainable by a peer.
//func maxScore() float64 {
//	totalWeight := decidedTopicWeight + subnetsTotalWeight
//	return (maxInMeshScore + maxFirstDeliveryScore) * totalWeight
//}
