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

			//filter all topics that have InvalidMessageDeliveries > 0
			filtered := make(map[string]*pubsub.TopicScoreSnapshot)
			for topic, snapshot := range peerScores.Topics {
				if snapshot.InvalidMessageDeliveries > 0 {
					filtered[topic] = snapshot
				}
			}
			// log peer overall score and topics scores
			logger.Debug("peer scores", fields.PeerID(pid),
				fields.PeerScore(peerScores.Score),
				zap.Any("invalid_messages", filtered),
				zap.Float64("ip_colocation", peerScores.IPColocationFactor),
				zap.Float64("behaviour_penalty", peerScores.BehaviourPenalty),
				zap.Float64("app_specific_penalty", peerScores.AppSpecificScore))

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
