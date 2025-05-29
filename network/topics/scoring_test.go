package topics

import (
	"testing"
	"time"

	pubsub "github.com/libp2p/go-libp2p-pubsub"
	"github.com/stretchr/testify/require"
)

func TestTruncateStats(t *testing.T) {
	// Test empty.
	filtered := []*topicScoreSnapshot{}
	log := formatInvalidMessageStats(filtered)
	require.Equal(t, "", log)

	// Test few subnets.
	filtered = []*topicScoreSnapshot{
		{
			"custom_topic",
			&pubsub.TopicScoreSnapshot{
				TimeInMesh:               0,
				FirstMessageDeliveries:   0,
				MeshMessageDeliveries:    0,
				InvalidMessageDeliveries: 3,
			},
		},
		{
			"ssv.v2.103",
			&pubsub.TopicScoreSnapshot{
				TimeInMesh:               0,
				FirstMessageDeliveries:   0,
				MeshMessageDeliveries:    0,
				InvalidMessageDeliveries: 3,
			},
		},
		{
			"ssv.v2.107",
			&pubsub.TopicScoreSnapshot{
				TimeInMesh:               3 * time.Second,
				FirstMessageDeliveries:   3.5,
				MeshMessageDeliveries:    2.666666,
				InvalidMessageDeliveries: 3.83,
			},
		},
		{
			"ssv.v2.109",
			&pubsub.TopicScoreSnapshot{
				TimeInMesh:               -20 * time.Millisecond,
				FirstMessageDeliveries:   -3.333333,
				MeshMessageDeliveries:    -2.25,
				InvalidMessageDeliveries: -3.83,
			},
		},
	}
	log = formatInvalidMessageStats(filtered)
	require.Equal(t, "custom_topic=0,0,0,3 103=0,0,0,3 107=3,3.5,2.67,3.83 109=-0.02,-3.33,-2.25,-3.83", log)
}
