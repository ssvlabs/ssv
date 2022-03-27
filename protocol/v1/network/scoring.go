package network

import pubsub "github.com/libp2p/go-libp2p-pubsub"

// DecidedTopicScoreParams returns the score params for decided topic
// TODO: complete
func DecidedTopicScoreParams() *pubsub.TopicScoreParams {
	return &pubsub.TopicScoreParams{
		TopicWeight:                     0,
		TimeInMeshWeight:                0,
		TimeInMeshQuantum:               0,
		TimeInMeshCap:                   0,
		FirstMessageDeliveriesWeight:    0,
		FirstMessageDeliveriesDecay:     0,
		FirstMessageDeliveriesCap:       0,
		MeshMessageDeliveriesWeight:     0,
		MeshMessageDeliveriesDecay:      0,
		MeshMessageDeliveriesCap:        0,
		MeshMessageDeliveriesThreshold:  0,
		MeshMessageDeliveriesWindow:     0,
		MeshMessageDeliveriesActivation: 0,
		MeshFailurePenaltyWeight:        0,
		MeshFailurePenaltyDecay:         0,
		InvalidMessageDeliveriesWeight:  0,
		InvalidMessageDeliveriesDecay:   0,
	}
}

// SubnetTopicScoreParams returns the score params for a subnet topic
// TODO: complete
func SubnetTopicScoreParams(name string) *pubsub.TopicScoreParams {
	return &pubsub.TopicScoreParams{
		TopicWeight:                     0,
		TimeInMeshWeight:                0,
		TimeInMeshQuantum:               0,
		TimeInMeshCap:                   0,
		FirstMessageDeliveriesWeight:    0,
		FirstMessageDeliveriesDecay:     0,
		FirstMessageDeliveriesCap:       0,
		MeshMessageDeliveriesWeight:     0,
		MeshMessageDeliveriesDecay:      0,
		MeshMessageDeliveriesCap:        0,
		MeshMessageDeliveriesThreshold:  0,
		MeshMessageDeliveriesWindow:     0,
		MeshMessageDeliveriesActivation: 0,
		MeshFailurePenaltyWeight:        0,
		MeshFailurePenaltyDecay:         0,
		InvalidMessageDeliveriesWeight:  0,
		InvalidMessageDeliveriesDecay:   0,
	}
}
