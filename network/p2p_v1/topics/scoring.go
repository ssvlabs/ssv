package topics

import (
	"github.com/bloxapp/ssv/network/p2p_v1/peers"
	"github.com/libp2p/go-libp2p-core/peer"
	pubsub "github.com/libp2p/go-libp2p-pubsub"
	"go.uber.org/zap"
	"math"
	"time"
)

const (
	defaultAppSpecificWeight  = 1.0
	defaultIPColocationWeight = -32.0
	defaultOneEpochDuration   = 12 * 64 * time.Second

	decayToZero = 0.01
)

// scoreInspector inspects scores and updates the score index accordingly
func scoreInspector(logger *zap.Logger, scoreIdx peers.ScoreIndex) func(scores map[peer.ID]*pubsub.PeerScoreSnapshot) {
	return func(scores map[peer.ID]*pubsub.PeerScoreSnapshot) {
		for pid, peerScores := range scores {
			err := scoreIdx.Score(pid, peers.NodeScore{
				Name:  "PS_Score",
				Value: peerScores.Score,
			}, peers.NodeScore{
				Name:  "PS_BehaviourPenalty",
				Value: peerScores.BehaviourPenalty,
			}, peers.NodeScore{
				Name:  "PS_IPColocationFactor",
				Value: peerScores.IPColocationFactor,
			})
			if err != nil {
				logger.Warn("could not score peer", zap.String("peer", pid.String()), zap.Error(err))
			} else {
				logger.Debug("peer scores were updated", zap.String("peer", pid.String()))
			}
		}
	}
}

// peerScoreThresholds returns the thresholds to use for peer scoring
// TODO: fine-tune values
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
// TODO: find-tune values
func peerScoreParams(cfg *PububConfig) *pubsub.PeerScoreParams {
	return &pubsub.PeerScoreParams{
		Topics:        make(map[string]*pubsub.TopicScoreParams),
		TopicScoreCap: 32.0,
		AppSpecificScore: appSpecificScore(
			cfg.Logger.With(zap.String("who", "appSpecificScore")),
			cfg.ScoreIndex),
		AppSpecificWeight:           cfg.Scoring.AppSpecificWeight,
		IPColocationFactorWeight:    cfg.Scoring.IPColocationWeight,
		IPColocationFactorThreshold: 10, // max 10 peers from the same IP
		IPColocationFactorWhitelist: cfg.Scoring.IPWhilelist,
		BehaviourPenaltyWeight:      -16.0,
		BehaviourPenaltyThreshold:   6,
		BehaviourPenaltyDecay:       scoreDecay(cfg.Scoring.OneEpochDuration*10, cfg.Scoring.OneEpochDuration),
		DecayInterval:               cfg.Scoring.OneEpochDuration,
		DecayToZero:                 decayToZero,
		RetainScore:                 cfg.Scoring.OneEpochDuration * 100,
	}
}

func appSpecificScore(logger *zap.Logger, scoreIdx peers.ScoreIndex) func(p peer.ID) float64 {
	return func(p peer.ID) float64 {
		// TODO: complete
		scores, err := scoreIdx.GetScore(p, "")
		if err != nil {
			logger.Warn("could not get score for peer", zap.String("peer", p.String()), zap.Error(err))
			return 0.0
		}
		var res float64
		for _, s := range scores {
			res += s.Value
		}
		return res
	}
}

//func decidedTopicScoreParams() *pubsub.TopicScoreParams {
//	pubsub.DefaultGossipSubParams()
//	return &pubsub.TopicScoreParams{
//		TopicWeight:                     0,
//		TimeInMeshWeight:                0,
//		TimeInMeshQuantum:               0,
//		TimeInMeshCap:                   0,
//		FirstMessageDeliveriesWeight:    0,
//		FirstMessageDeliveriesDecay:     0,
//		FirstMessageDeliveriesCap:       0,
//		MeshMessageDeliveriesWeight:     0,
//		MeshMessageDeliveriesDecay:      0,
//		MeshMessageDeliveriesCap:        0,
//		MeshMessageDeliveriesThreshold:  0,
//		MeshMessageDeliveriesWindow:     0,
//		MeshMessageDeliveriesActivation: 0,
//		MeshFailurePenaltyWeight:        0,
//		MeshFailurePenaltyDecay:         0,
//		InvalidMessageDeliveriesWeight:  0,
//		InvalidMessageDeliveriesDecay:   0,
//	}
//}
