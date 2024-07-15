package topics

import (
	"math"
	"time"

	"github.com/ssvlabs/ssv/logging/fields"
	"github.com/ssvlabs/ssv/network/commons"

	pubsub "github.com/libp2p/go-libp2p-pubsub"
	"github.com/libp2p/go-libp2p/core/peer"
	"go.uber.org/zap"

	"github.com/ssvlabs/ssv/network/peers"
	"github.com/ssvlabs/ssv/network/topics/params"
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

		// Get validator stats
		totalValidators, activeValidators, myValidators, err := cfg.GetValidatorStats()
		if err != nil {
			logger.Debug("could not read stats: active validators")
			return nil
		}
		logger := logger.With(zap.String("topic", t), zap.Uint64("totalValidators", totalValidators),
			zap.Uint64("activeValidators", activeValidators), zap.Uint64("myValidators", myValidators))
		logger.Debug("got validator stats for score params")

		// Get committee maps
		TopicCommitteeIDsMap, CommitteeIDToOperatorsMap, CommitteeIDToValidatorsMap, err := cfg.GetCommitteeMapsForTopic()
		if err != nil {
			logger.Debug("could not read validators maps", zap.Error(err))
			return nil
		}

		committeeOperators, committeeValidators := filterCommitteeMapsForTopic(t, TopicCommitteeIDsMap, CommitteeIDToOperatorsMap, CommitteeIDToValidatorsMap)

		validatorsInTopic := 0
		for _, validators := range committeeValidators {
			validatorsInTopic += validators
		}
		logger = logger.With(zap.Int("committees in topic", len(committeeOperators)), zap.Int("validators in topic", validatorsInTopic))
		logger.Debug("got committee maps for score params")

		// Create topic options
		opts, err := params.NewSubnetTopicOpts(int(totalValidators), commons.Subnets(), committeeOperators, committeeValidators)
		if err != nil {
			logger.Debug("could not get subnet topic options", zap.Error(err))
			return nil
		}

		// Generate topic parameters
		tp, err := params.TopicParams(opts)
		if err != nil {
			logger.Debug("ignoring topic score params", zap.Error(err))
			return nil
		}
		return tp
	}
}

// Filters the committeeOperators and committeeValidators maps only for the committees that belong to the given topic
// according to the topicCommittees map
func filterCommitteeMapsForTopic(topic string, topicCommittees map[string][]string, committeeOperators map[string]int, committeeValidators map[string]int) (map[string]int, map[string]int) {

	filteredCommitteeOperators := make(map[string]int)
	filteredCommitteeValidators := make(map[string]int)

	committeesInTopic, exists := topicCommittees[topic]
	if exists {
		//  For each committee in the topic, set its operators and validators
		for _, committeeID := range committeesInTopic {
			operators, exists := committeeOperators[committeeID]
			if !exists {
				continue
			}
			validators, exists := committeeValidators[committeeID]
			if !exists {
				continue
			}
			filteredCommitteeOperators[committeeID] = operators
			filteredCommitteeValidators[committeeID] = validators
		}
	}

	return filteredCommitteeOperators, filteredCommitteeValidators
}
