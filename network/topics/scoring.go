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
			metricPubsubPeerScoreInspect.WithLabelValues(pid.String()).Set(peerScores.Score)
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
		totalValidators, activeValidators, myValidators, err := cfg.GetValidatorStats()
		if err != nil {
			cfg.Logger.Debug("could not read stats: active validators")
			return nil
		}
		logger := cfg.Logger.With(zap.String("topic", t), zap.Uint64("totalValidators", totalValidators),
			zap.Uint64("activeValidators", activeValidators), zap.Uint64("myValidators", myValidators))
		logger.Debug("got validator stats for score params")
		var opts params.Options
		switch t {
		case decidedTopic:
			opts = params.NewDecidedTopicOpts(int(totalValidators), f.Subnets())
		default:
			opts = params.NewSubnetTopicOpts(int(totalValidators), f.Subnets())
		}
		tp, err := params.TopicParams(opts)
		if err != nil {
			logger.Debug("ignoring topic score params", zap.Error(err))
			return nil
		}
		return tp
	}
}
