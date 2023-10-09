package topics

import (
	"time"

	"github.com/bloxapp/ssv/logging/fields"
	"github.com/bloxapp/ssv/network/commons"

	pubsub "github.com/libp2p/go-libp2p-pubsub"
	"github.com/libp2p/go-libp2p/core/peer"
	"go.uber.org/zap"

	"github.com/bloxapp/ssv/network/peers"
	"github.com/bloxapp/ssv/network/topics/params"
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
		for pid, peerScores := range scores {
			// scores := []*peers.NodeScore{
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
			logger.Debug("peer scores", fields.PeerID(pid),
				zap.Any("peerScores", peerScores))
			metricPubsubPeerScoreInspect.WithLabelValues(pid.String()).Set(peerScores.Score)
			// err := scoreIdx.Score(pid, scores...)
			// if err != nil {
			//	logger.Warn("could not score peer", zap.String("peer", pid.String()), zap.Error(err))
			// } else {
			//	logger.Debug("peer scores were updated", zap.String("peer", pid.String()),
			//		zap.Any("scores", scores), zap.Any("topicScores", peerScores.Topics))
			//}
		}
	}
}

// topicScoreParams factory for creating scoring params for topics
func topicScoreParams(logger *zap.Logger, cfg *PubSubConfig) func(string) *pubsub.TopicScoreParams {
	return func(t string) *pubsub.TopicScoreParams {
		totalValidators, activeValidators, myValidators, err := cfg.GetValidatorStats()
		if err != nil {
			logger.Debug("could not read stats: active validators")
			return nil
		}
		logger := logger.With(zap.String("topic", t), zap.Uint64("totalValidators", totalValidators),
			zap.Uint64("activeValidators", activeValidators), zap.Uint64("myValidators", myValidators))
		logger.Debug("got validator stats for score params")
		opts := params.NewSubnetTopicOpts(int(totalValidators), commons.Subnets())
		tp, err := params.TopicParams(opts)
		if err != nil {
			logger.Debug("ignoring topic score params", zap.Error(err))
			return nil
		}
		return tp
	}
}
