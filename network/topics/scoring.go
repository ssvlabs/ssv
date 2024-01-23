package topics

import (
	"math"
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
func scoreInspector(logger *zap.Logger, scoreIdx peers.ScoreIndex, logFrequency int, metrics Metrics, peerConnected func(pid peer.ID) bool) pubsub.ExtendedPeerScoreInspectFn {
	inspections := 0

	return func(scores map[peer.ID]*pubsub.PeerScoreSnapshot) {
		// Reset metrics before updating them.
		metrics.ResetPeerScores()

		for pid, peerScores := range scores {
			// Compute score-related stats for this peer.
			filtered := make(map[string]*pubsub.TopicScoreSnapshot)
			var totalInvalidMessages float64
			var totalLowMeshDeliveries int
			var p4ScoreSquaresSum float64
			for topic, snapshot := range peerScores.Topics {
				p4ScoreSquaresSum += snapshot.InvalidMessageDeliveries * snapshot.InvalidMessageDeliveries

				if snapshot.InvalidMessageDeliveries != 0 {
					filtered[topic] = snapshot
				}
				if snapshot.InvalidMessageDeliveries > 0 {
					totalInvalidMessages += math.Sqrt(snapshot.InvalidMessageDeliveries)
				}
				if snapshot.MeshMessageDeliveries < 107 {
					totalLowMeshDeliveries++
				}
			}

			// Update metrics.
			metrics.PeerScore(pid, peerScores.Score)
			metrics.PeerP4Score(pid, p4ScoreSquaresSum)

			if inspections%logFrequency != 0 {
				// Don't log yet.
				continue
			}

			// Log.
			fields := []zap.Field{
				fields.PeerID(pid),
				fields.PeerScore(peerScores.Score),
				zap.Any("invalid_messages", filtered),
				zap.Float64("ip_colocation", peerScores.IPColocationFactor),
				zap.Float64("behaviour_penalty", peerScores.BehaviourPenalty),
				zap.Float64("app_specific_penalty", peerScores.AppSpecificScore),
				zap.Float64("total_low_mesh_deliveries", float64(totalLowMeshDeliveries)),
				zap.Float64("total_invalid_messages", totalInvalidMessages),
				zap.Any("invalid_messages", filtered),
			}
			if peerConnected(pid) {
				fields = append(fields, zap.Bool("connected", true))
			}
			if peerScores.Score < -1000 {
				fields = append(fields, zap.Bool("low_score", true))
			}
			logger.Debug("peer scores", fields...)

			// err := scoreIdx.Score(pid, scores...)
			// if err != nil {
			//	logger.Warn("could not score peer", zap.String("peer", pid.String()), zap.Error(err))
			// } else {
			//	logger.Debug("peer scores were updated", zap.String("peer", pid.String()),
			//		zap.Any("scores", scores), zap.Any("topicScores", peerScores.Topics))
			//}
		}

		inspections++
	}
}

// topicScoreParams factory for creating scoring params for topics
func topicScoreParams(logger *zap.Logger, cfg *PubSubConfig) func(string) *pubsub.TopicScoreParams {
	return func(t string) *pubsub.TopicScoreParams {
		validatorStats := cfg.GetValidatorStats()

		logger := logger.With(zap.String("topic", t), zap.Uint64("totalValidators", validatorStats.TotalShares),
			zap.Uint64("activeValidators", validatorStats.ActiveValidators), zap.Uint64("myValidators", validatorStats.OperatorShares))
		logger.Debug("got validator stats for score params")

		opts := params.NewSubnetTopicOpts(int(validatorStats.TotalShares), commons.Subnets())
		tp, err := params.TopicParams(opts)
		if err != nil {
			logger.Debug("ignoring topic score params", zap.Error(err))
			return nil
		}
		return tp
	}
}
