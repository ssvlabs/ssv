package topics

import (
	"time"

	"github.com/ssvlabs/ssv/logging/fields"
	"github.com/ssvlabs/ssv/network/commons"
	"github.com/ssvlabs/ssv/registry/storage"

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
func scoreInspector(logger *zap.Logger, scoreIdx peers.ScoreIndex, logFrequency int, metrics Metrics, peerConnected func(pid peer.ID) bool, peerScoreParams *pubsub.PeerScoreParams, topicScoreParamsFactory func(string) *pubsub.TopicScoreParams) pubsub.ExtendedPeerScoreInspectFn {
	inspections := 0

	return func(scores map[peer.ID]*pubsub.PeerScoreSnapshot) {

		if inspections%logFrequency != 0 {
			// Don't log yet.
			inspections++
			return
		}

		// Reset metrics before updating them.
		metrics.ResetPeerScores()

		// Use a "scope cache" for getting a topic's score parameters
		// otherwise, the factory method would be called multiple times for the same topic
		topicScoreParamsCache := make(map[string]*pubsub.TopicScoreParams)
		getScoreParamsForTopic := func(topic string) *pubsub.TopicScoreParams {
			if topicParams, exists := topicScoreParamsCache[topic]; exists {
				return topicParams
			}
			topicScoreParamsCache[topic] = topicScoreParamsFactory(topic)
			return topicScoreParamsCache[topic]
		}

		// Log score for each peer
		for pid, peerScores := range scores {

			// Store topic snapshot for topics with invalid messages
			filtered := make(map[string]*pubsub.TopicScoreSnapshot)
			for topic, snapshot := range peerScores.Topics {
				if snapshot.InvalidMessageDeliveries != 0 {
					filtered[topic] = snapshot
				}
			}

			// Compute p4 impact in final score for metrics
			p4Impact := float64(0)

			// Compute counters and weights for p1, p2, p4, p6 and p7.
			// The subscores p3 and p5 are unused.

			// P1 - Time in mesh
			// The weight should be equal for all topics. So, we can just sum up the counters.
			p1CounterSum := float64(0)

			// P2 - First message deliveries
			// The weight for the p2 score (w2) may be different through topics
			// So, we store a list of counters P2c and their associated weights W2
			// Note: if necessary, we may reduce the size of the log by summing P2c * W2 for all topics and logging this result
			type P2Score struct {
				P2Counter float64
				W2        float64
			}
			p2 := make([]*P2Score, 0)

			// P4 - InvalidMessageDeliveries
			// The weight should be equal for all topics. So, we can just sum up the counters squared.
			p4CounterSum := float64(0)

			// Get counters for each topic
			for topic, snapshot := range peerScores.Topics {

				topicScoreParams := getScoreParamsForTopic(topic)

				// Cap p1 as done in GossipSub
				p1Counter := float64(snapshot.TimeInMesh / topicScoreParams.TimeInMeshQuantum)
				if p1Counter > topicScoreParams.TimeInMeshCap {
					p1Counter = topicScoreParams.TimeInMeshCap
				}
				p1CounterSum += p1Counter

				// Square the P4 counter as done in GossipSub
				p4CounterSquaredForTopic := snapshot.InvalidMessageDeliveries * snapshot.InvalidMessageDeliveries
				p4CounterSum += p4CounterSquaredForTopic

				// Update p4 impact on final score
				p4Impact += topicScoreParams.TopicWeight * topicScoreParams.InvalidMessageDeliveriesWeight * p4CounterSquaredForTopic

				// Store the counter and weight for P2
				w2 := topicScoreParams.FirstMessageDeliveriesWeight
				p2 = append(p2, &P2Score{
					P2Counter: snapshot.FirstMessageDeliveries,
					W2:        w2,
				})
			}

			// Get weights for P1 and P4 (w1 and w4), which should be equal for all topics
			w1 := float64(0)
			w4 := float64(0)
			for topic := range peerScores.Topics {
				topicScoreParams := getScoreParamsForTopic(topic)
				w1 = topicScoreParams.TimeInMeshWeight
				w4 = topicScoreParams.InvalidMessageDeliveriesWeight
				break
			}

			// P6 - IP Colocation factor
			p6 := peerScores.IPColocationFactor
			w6 := peerScoreParams.IPColocationFactorWeight

			// P7 - Behaviour penalty
			p7 := peerScores.BehaviourPenalty
			w7 := peerScoreParams.BehaviourPenaltyWeight

			// Update metrics.
			metrics.PeerScore(pid, peerScores.Score)
			metrics.PeerP4Score(pid, p4Impact)

			// Log.
			fields := []zap.Field{
				fields.PeerID(pid),
				fields.PeerScore(peerScores.Score),
				zap.Float64("p1_time_in_mesh", p1CounterSum),
				zap.Float64("w1_time_in_mesh", w1),
				zap.Any("p2_first_message_deliveries", p2),
				zap.Float64("p4_invalid_message_deliveries", p4CounterSum),
				zap.Float64("w4_invalid_message_deliveries", w4),
				zap.Float64("p6_ip_colocation_factor", p6),
				zap.Float64("w6_ip_colocation_factor", w6),
				zap.Float64("p7_behaviour_penalty", p7),
				zap.Float64("w7_behaviour_penalty", w7),
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
func topicScoreParams(logger *zap.Logger, cfg *PubSubConfig, committeesProvider CommitteesProvider) func(string) *pubsub.TopicScoreParams {
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

		// Get committees
		committees := committeesProvider.Committees()
		topicCommittees := filterCommitteesForTopic(t, committees)

		// Log
		validatorsInTopic := 0
		for _, committee := range topicCommittees {
			validatorsInTopic += len(committee.Validators)
		}
		committeesInTopic := len(topicCommittees)
		logger = logger.With(zap.Int("committees in topic", committeesInTopic), zap.Int("validators in topic", validatorsInTopic))
		logger.Debug("got filtered committees for score params")

		// Create topic options
		opts := params.NewSubnetTopicOpts(int(totalValidators), commons.Subnets(), topicCommittees)

		// Generate topic parameters
		tp, err := params.TopicParams(opts)
		if err != nil {
			logger.Debug("ignoring topic score params", zap.Error(err))
			return nil
		}
		return tp
	}
}

// topicScoreParams factory for creating scoring params for topics
func validatorTopicScoreParams(logger *zap.Logger, cfg *PubSubConfig) func(string) *pubsub.TopicScoreParams {
	return func(t string) *pubsub.TopicScoreParams {
		totalValidators, activeValidators, myValidators, err := cfg.GetValidatorStats()
		if err != nil {
			logger.Debug("could not read stats: active validators")
			return nil
		}
		logger := logger.With(zap.String("topic", t), zap.Uint64("totalValidators", totalValidators),
			zap.Uint64("activeValidators", activeValidators), zap.Uint64("myValidators", myValidators))
		logger.Debug("got validator stats for score params")
		opts := params.NewSubnetTopicOptsValidators(int(totalValidators), commons.Subnets())
		tp, err := params.TopicParams(opts)
		if err != nil {
			logger.Debug("ignoring topic score params", zap.Error(err))
			return nil
		}
		return tp
	}
}

// Returns a new committee list with only the committees that belong to the given topic
func filterCommitteesForTopic(topic string, committees []*storage.Committee) []*storage.Committee {

	topicCommittees := make([]*storage.Committee, 0)

	for _, committee := range committees {
		// Get topic
		subnet := commons.CommitteeSubnet(committee.ID)
		committeeTopic := commons.SubnetTopicID(subnet)
		committeeTopicFullName := commons.GetTopicFullName(committeeTopic)

		// If it belongs to the topic, add it
		if topic == committeeTopicFullName {
			topicCommittees = append(topicCommittees, committee)
		}
	}
	return topicCommittees
}
